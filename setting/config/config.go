package config

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// ConfigManager 统一管理所有配置
type ConfigManager struct {
	configs map[string]interface{}
	mutex   sync.RWMutex
}

var GlobalConfig = NewConfigManager()

func NewConfigManager() *ConfigManager {
	return &ConfigManager{
		configs: make(map[string]interface{}),
	}
}

// Register 注册一个配置模块
func (cm *ConfigManager) Register(name string, config interface{}) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	cm.configs[name] = config
}

// Get 获取指定配置模块
func (cm *ConfigManager) Get(name string) interface{} {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	return cm.configs[name]
}

func cloneConfigValue(config interface{}) (interface{}, error) {
	if config == nil {
		return nil, nil
	}

	valueType := reflect.TypeOf(config)
	if valueType.Kind() != reflect.Ptr || valueType.Elem().Kind() != reflect.Struct {
		return config, nil
	}

	raw, err := common.Marshal(config)
	if err != nil {
		return nil, err
	}

	target := reflect.New(valueType.Elem()).Interface()
	if err := common.UnmarshalWithNumber(raw, target); err != nil {
		return nil, err
	}

	return target, nil
}

// Read executes fn with a snapshot of the named config after releasing the manager read lock.
func (cm *ConfigManager) Read(name string, fn func(config interface{})) {
	if fn == nil {
		return
	}
	cm.mutex.RLock()
	config, err := cloneConfigValue(cm.configs[name])
	cm.mutex.RUnlock()
	if err != nil {
		common.SysError("failed to clone config " + name + ": " + err.Error())
		return
	}
	fn(config)
}

// ReadLive executes fn with the live config object while the manager read lock is
// held. Use it for hot-path field reads: the alternative raw-pointer getters read
// a struct that Update rewrites field by field, which is a data race.
//
// fn runs under the read lock, so it must only inspect the object: no blocking
// calls, and no re-entry into ConfigManager.
func (cm *ConfigManager) ReadLive(name string, fn func(config interface{})) bool {
	if fn == nil {
		return false
	}
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	config := cm.configs[name]
	if config == nil {
		return false
	}
	fn(config)
	return true
}

// Update applies a map of "<field json tag>" -> string values to a registered
// config module under the manager write lock.
//
// Every production write must go through here (or LoadFromDB): applying the map
// unlocked races with ReadLive/Read/ExportAllConfigs, which observe the same
// struct. Returns an error for an unregistered module so callers cannot believe
// a write landed when nothing was updated.
func (cm *ConfigManager) Update(name string, configMap map[string]string) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	config, ok := cm.configs[name]
	if !ok || config == nil {
		return fmt.Errorf("config module %q is not registered", name)
	}
	return updateConfigFromMap(config, configMap)
}

// LoadFromDB 从数据库加载配置
func (cm *ConfigManager) LoadFromDB(options map[string]string) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	for name, config := range cm.configs {
		prefix := name + "."
		configMap := make(map[string]string)

		// 收集属于此配置的所有选项
		for key, value := range options {
			if strings.HasPrefix(key, prefix) {
				configKey := strings.TrimPrefix(key, prefix)
				configMap[configKey] = value
			}
		}

		// 如果找到配置项，则更新配置
		if len(configMap) > 0 {
			if err := updateConfigFromMap(config, configMap); err != nil {
				common.SysError("failed to update config " + name + ": " + err.Error())
				continue
			}
		}
	}

	return nil
}

// SaveToDB 将配置保存到数据库
func (cm *ConfigManager) SaveToDB(updateFunc func(key, value string) error) error {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	for name, config := range cm.configs {
		configMap, err := configToMap(config)
		if err != nil {
			return err
		}

		for key, value := range configMap {
			dbKey := name + "." + key
			if err := updateFunc(dbKey, value); err != nil {
				return err
			}
		}
	}

	return nil
}

// 辅助函数：将配置对象转换为map
func configToMap(config interface{}) (map[string]string, error) {
	result := make(map[string]string)

	val := reflect.ValueOf(config)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, nil
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// 跳过未导出字段
		if !fieldType.IsExported() {
			continue
		}

		// 获取json标签作为键名
		key := fieldType.Tag.Get("json")
		if key == "" || key == "-" {
			key = fieldType.Name
		}

		// 处理不同类型的字段
		var strValue string
		switch field.Kind() {
		case reflect.String:
			strValue = field.String()
		case reflect.Bool:
			strValue = strconv.FormatBool(field.Bool())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			strValue = strconv.FormatInt(field.Int(), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			strValue = strconv.FormatUint(field.Uint(), 10)
		case reflect.Float32, reflect.Float64:
			strValue = strconv.FormatFloat(field.Float(), 'f', -1, 64)
		case reflect.Ptr:
			// 处理指针类型：如果非 nil，序列化指向的值
			if !field.IsNil() {
				bytes, err := common.Marshal(field.Interface())
				if err != nil {
					return nil, err
				}
				strValue = string(bytes)
			} else {
				// nil 指针序列化为 "null"
				strValue = "null"
			}
		case reflect.Map, reflect.Slice, reflect.Struct:
			// 复杂类型使用JSON序列化
			bytes, err := common.Marshal(field.Interface())
			if err != nil {
				return nil, err
			}
			strValue = string(bytes)
		default:
			// 跳过不支持的类型
			continue
		}

		result[key] = strValue
	}

	return result, nil
}

// 辅助函数：从map更新配置对象
func updateConfigFromMap(config interface{}, configMap map[string]string) error {
	val := reflect.ValueOf(config)
	if val.Kind() != reflect.Ptr {
		return nil
	}
	val = val.Elem()

	if val.Kind() != reflect.Struct {
		return nil
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// 跳过未导出字段
		if !fieldType.IsExported() {
			continue
		}

		// 获取json标签作为键名
		key := fieldType.Tag.Get("json")
		if key == "" || key == "-" {
			key = fieldType.Name
		}

		// 检查map中是否有对应的值
		strValue, ok := configMap[key]
		if !ok {
			continue
		}

		// 根据字段类型设置值
		if !field.CanSet() {
			continue
		}

		switch field.Kind() {
		case reflect.String:
			field.SetString(strValue)
		case reflect.Bool:
			boolValue, err := strconv.ParseBool(strValue)
			if err != nil {
				continue
			}
			field.SetBool(boolValue)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			intValue, err := strconv.ParseInt(strValue, 10, 64)
			if err != nil {
				// 兼容 float 格式的字符串（如 "2.000000"）
				floatValue, fErr := strconv.ParseFloat(strValue, 64)
				if fErr != nil {
					continue
				}
				intValue = int64(floatValue)
			}
			field.SetInt(intValue)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			uintValue, err := strconv.ParseUint(strValue, 10, 64)
			if err != nil {
				// 兼容 float 格式的字符串
				floatValue, fErr := strconv.ParseFloat(strValue, 64)
				if fErr != nil || floatValue < 0 {
					continue
				}
				uintValue = uint64(floatValue)
			}
			field.SetUint(uintValue)
		case reflect.Float32, reflect.Float64:
			floatValue, err := strconv.ParseFloat(strValue, 64)
			if err != nil {
				continue
			}
			field.SetFloat(floatValue)
		case reflect.Ptr:
			// 处理指针类型
			if strValue == "null" {
				field.Set(reflect.Zero(field.Type()))
			} else {
				// 如果指针是 nil，需要先初始化
				if field.IsNil() {
					field.Set(reflect.New(field.Type().Elem()))
				}
				// 反序列化到指针指向的值
				err := common.UnmarshalJsonStr(strValue, field.Interface())
				if err != nil {
					continue
				}
			}
		case reflect.Map:
			fresh := reflect.New(field.Type())
			if err := common.UnmarshalJsonStr(strValue, fresh.Interface()); err != nil {
				continue
			}
			field.Set(fresh.Elem())
		case reflect.Slice:
			// json 解码会复用非 nil 切片的底层数组并原地覆盖元素，已持有旧切片头
			// 的读者会读到撕裂数据。切片一律解码到新值再整体替换发布，使旧对象
			// 在替换后不再被改写。
			fresh := reflect.New(field.Type())
			if err := common.UnmarshalJsonStr(strValue, fresh.Interface()); err != nil {
				continue
			}
			field.Set(fresh.Elem())
		case reflect.Struct:
			if err := common.UnmarshalJsonStr(strValue, field.Addr().Interface()); err != nil {
				continue
			}
		}
	}

	return nil
}

// ConfigToMap 将配置对象转换为map（导出函数）
func ConfigToMap(config interface{}) (map[string]string, error) {
	return configToMap(config)
}

// UpdateConfigFromMap 从map更新配置对象（导出函数）。
//
// Deprecated: 该函数不持管理器锁，直接对注册配置调用它会与
// Read/ReadLive/ExportAllConfigs 构成数据竞争。注册配置的写入一律走
// ConfigManager.Update；此导出形式仅供测试直接操作自有的结构体。
func UpdateConfigFromMap(config interface{}, configMap map[string]string) error {
	return updateConfigFromMap(config, configMap)
}

// ExportAllConfigs 导出所有已注册的配置为扁平结构
func (cm *ConfigManager) ExportAllConfigs() map[string]string {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	result := make(map[string]string)

	for name, cfg := range cm.configs {
		configMap, err := ConfigToMap(cfg)
		if err != nil {
			continue
		}

		// 使用 "模块名.配置项" 的格式添加到结果中
		for key, value := range configMap {
			result[name+"."+key] = value
		}
	}

	return result
}
