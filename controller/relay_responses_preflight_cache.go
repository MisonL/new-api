package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type responsesPreflightCache struct {
	Settings       map[int]dto.ChannelSettings
	OtherSettings  map[int]dto.ChannelOtherSettings
	ParamOverrides map[int]map[string]interface{}
}

func getResponsesPreflightCache(c *gin.Context) *responsesPreflightCache {
	if c == nil {
		return nil
	}
	if cache, ok := common.GetContextKeyType[*responsesPreflightCache](c, constant.ContextKeyResponsesPreflightCache); ok && cache != nil {
		return cache
	}
	cache := &responsesPreflightCache{
		Settings:       make(map[int]dto.ChannelSettings),
		OtherSettings:  make(map[int]dto.ChannelOtherSettings),
		ParamOverrides: make(map[int]map[string]interface{}),
	}
	common.SetContextKey(c, constant.ContextKeyResponsesPreflightCache, cache)
	return cache
}

func cachedResponsesChannelSettings(c *gin.Context, channel *model.Channel) dto.ChannelSettings {
	if channel == nil {
		return dto.ChannelSettings{}
	}
	if channel.Id <= 0 {
		return channel.GetSetting()
	}
	cache := getResponsesPreflightCache(c)
	if cache == nil {
		return channel.GetSetting()
	}
	if settings, ok := cache.Settings[channel.Id]; ok {
		return settings
	}
	settings := channel.GetSetting()
	cache.Settings[channel.Id] = settings
	return settings
}

func cachedResponsesChannelOtherSettings(c *gin.Context, channel *model.Channel) dto.ChannelOtherSettings {
	if channel == nil {
		return dto.ChannelOtherSettings{}
	}
	if channel.Id <= 0 {
		return channel.GetOtherSettings()
	}
	cache := getResponsesPreflightCache(c)
	if cache == nil {
		return channel.GetOtherSettings()
	}
	if settings, ok := cache.OtherSettings[channel.Id]; ok {
		return settings
	}
	settings := channel.GetOtherSettings()
	cache.OtherSettings[channel.Id] = settings
	return settings
}

func cachedResponsesChannelParamOverride(c *gin.Context, channel *model.Channel) map[string]interface{} {
	if channel == nil {
		return nil
	}
	if channel.Id <= 0 {
		return channel.GetParamOverride()
	}
	cache := getResponsesPreflightCache(c)
	if cache == nil {
		return channel.GetParamOverride()
	}
	if params, ok := cache.ParamOverrides[channel.Id]; ok {
		return params
	}
	params := channel.GetParamOverride()
	cache.ParamOverrides[channel.Id] = params
	return params
}
