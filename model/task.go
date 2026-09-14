package model

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"net/url"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	commonRelay "github.com/QuantumNous/new-api/relay/common"
)

type TaskStatus string

func (t TaskStatus) ToVideoStatus() string {
	var status string
	switch t {
	case TaskStatusQueued, TaskStatusSubmitted:
		status = dto.VideoStatusQueued
	case TaskStatusInProgress:
		status = dto.VideoStatusInProgress
	case TaskStatusSuccess:
		status = dto.VideoStatusCompleted
	case TaskStatusFailure:
		status = dto.VideoStatusFailed
	default:
		status = dto.VideoStatusUnknown // Default fallback
	}
	return status
}

const (
	TaskStatusNotStart   TaskStatus = "NOT_START"
	TaskStatusSubmitted             = "SUBMITTED"
	TaskStatusQueued                = "QUEUED"
	TaskStatusInProgress            = "IN_PROGRESS"
	TaskStatusFailure               = "FAILURE"
	TaskStatusSuccess               = "SUCCESS"
	TaskStatusUnknown               = "UNKNOWN"
)

// IsTaskTerminalStatus reports whether a task has reached an irreversible
// billing/lifecycle state. Once a task is terminal, later provider responses
// may refresh metadata but must never change SUCCESS into FAILURE (or the
// reverse).
func IsTaskTerminalStatus(status TaskStatus) bool {
	return status == TaskStatusSuccess || status == TaskStatusFailure
}

// IsTaskStatusTransitionAllowed enforces the monotonic task lifecycle shared
// by asynchronous pollers and webhook handlers. Provider APIs occasionally
// return stale queued/in-progress responses after a newer response, so
// non-terminal regressions are ignored as well as terminal reversals.
func IsTaskStatusTransitionAllowed(from TaskStatus, to TaskStatus) bool {
	from = TaskStatus(strings.ToUpper(strings.TrimSpace(string(from))))
	to = TaskStatus(strings.ToUpper(strings.TrimSpace(string(to))))
	if to == "" || to == TaskStatusUnknown {
		return false
	}
	if from == to {
		return true
	}
	if IsTaskTerminalStatus(from) {
		return false
	}
	if from == "" || from == TaskStatusUnknown {
		return taskStatusProgressRank(to) >= 0
	}
	fromRank := taskStatusProgressRank(from)
	toRank := taskStatusProgressRank(to)
	if fromRank < 0 || toRank < 0 {
		return false
	}
	return toRank >= fromRank
}

func taskStatusProgressRank(status TaskStatus) int {
	switch status {
	case TaskStatusNotStart:
		return 0
	case TaskStatusSubmitted:
		return 1
	case TaskStatusQueued:
		return 2
	case TaskStatusInProgress:
		return 3
	case TaskStatusSuccess, TaskStatusFailure:
		return 4
	default:
		return -1
	}
}

type Task struct {
	ID        int64 `json:"id" gorm:"primary_key;AUTO_INCREMENT;index:idx_tasks_progress_status_id,priority:3"`
	CreatedAt int64 `json:"created_at" gorm:"index"`
	UpdatedAt int64 `json:"updated_at"`
	// 第三方id，不一定有/ song id\ Task id
	TaskID    string                `json:"task_id" gorm:"type:varchar(191);index"`
	Platform  constant.TaskPlatform `json:"platform" gorm:"type:varchar(30);index"` // 平台
	UserId    int                   `json:"user_id" gorm:"index"`
	Group     string                `json:"group" gorm:"type:varchar(50)"` // 修正计费用
	ChannelId int                   `json:"channel_id" gorm:"index"`
	Quota     int                   `json:"quota"`
	// 任务类型, song, lyrics, description-mode
	Action string `json:"action" gorm:"type:varchar(40);index"`
	// 任务状态
	Status     TaskStatus `json:"status" gorm:"type:varchar(20);index;index:idx_tasks_progress_status_submit,priority:2;index:idx_tasks_progress_status_id,priority:2"`
	FailReason string     `json:"fail_reason"`
	SubmitTime int64      `json:"submit_time" gorm:"index;index:idx_tasks_progress_status_submit,priority:3"`
	StartTime  int64      `json:"start_time" gorm:"index"`
	FinishTime int64      `json:"finish_time" gorm:"index"`
	Progress   string     `json:"progress" gorm:"type:varchar(20);index;index:idx_tasks_progress_status_submit,priority:1;index:idx_tasks_progress_status_id,priority:1"`
	Properties Properties `json:"properties" gorm:"type:json"`
	Username   string     `json:"username,omitempty" gorm:"-"`
	// 禁止返回给用户，内部可能包含key等隐私信息
	PrivateData TaskPrivateData `json:"-" gorm:"column:private_data;type:json"`
	Data        json.RawMessage `json:"data" gorm:"type:json"`
}

func (t *Task) SetData(data any) {
	b, _ := common.Marshal(data)
	t.Data = json.RawMessage(b)
}

func (t *Task) GetData(v any) error {
	return common.Unmarshal(t.Data, &v)
}

type Properties struct {
	Input             string `json:"input"`
	UpstreamModelName string `json:"upstream_model_name,omitempty"`
	OriginModelName   string `json:"origin_model_name,omitempty"`
}

func (m *Properties) Scan(val interface{}) error {
	bytesValue, _ := val.([]byte)
	if len(bytesValue) == 0 {
		*m = Properties{}
		return nil
	}
	return common.Unmarshal(bytesValue, m)
}

func (m Properties) Value() (driver.Value, error) {
	if m == (Properties{}) {
		return nil, nil
	}
	return common.Marshal(m)
}

type TaskPrivateData struct {
	// Key is kept in memory for the polling code and for backwards-compatible
	// reads of legacy rows. New database writes are encoded by Value and never
	// persist this field in plaintext.
	Key            string `json:"key,omitempty"`
	KeyCiphertext  string `json:"key_ciphertext,omitempty"`
	UpstreamTaskID string `json:"upstream_task_id,omitempty"` // 上游真实 task ID
	ResultURL      string `json:"result_url,omitempty"`       // 任务成功后的结果 URL（视频地址等）
	// RouteSnapshot freezes the route identity selected at submission time.
	// Follow-up polling and content downloads must not resolve the channel's
	// current mapping, operation path, or header profile again.
	RouteSnapshot *TaskRouteSnapshot `json:"route_snapshot,omitempty"`
	// 计费上下文：用于异步退款/差额结算（轮询阶段读取）
	BillingSource  string              `json:"billing_source,omitempty"`  // "wallet" 或 "subscription"
	SubscriptionId int                 `json:"subscription_id,omitempty"` // 订阅 ID，用于订阅退款
	TokenId        int                 `json:"token_id,omitempty"`        // 令牌 ID，用于令牌额度退款
	BillingContext *TaskBillingContext `json:"billing_context,omitempty"` // 计费参数快照（用于轮询阶段重新计算）
}

// TaskRouteSnapshotSchemaVersion is the current persisted snapshot schema.
// Version 5 adds authenticated header-policy metadata. Version 6 adds the
// provider runtime snapshot so asynchronous follow-ups do not re-read mutable
// channel-specific parameters after submission. New snapshots also
// encrypt the resolved static outbound headers so asynchronous follow-ups
// replay the exact submission identity instead of re-reading mutable channel
// or tag policy. Header values never appear in plaintext in the task row; the
// encrypted policy ciphertext and its authenticated fingerprint are persisted.
const (
	// TaskRouteSnapshotHeaderPolicySchemaVersion is the first schema that
	// persists an authenticated copy of the resolved static header policy.
	// Versions at or above this boundary must never re-read the live Header
	// Profile during an asynchronous follow-up.
	TaskRouteSnapshotHeaderPolicySchemaVersion = 5
	TaskRouteSnapshotSchemaVersion             = 6
)

// TaskRouteProviderSnapshot freezes the non-secret provider runtime inputs that
// are otherwise read from a mutable Channel row during request setup. Proxy
// and resolved header values are deliberately excluded: they have dedicated
// authenticated encrypted fields in TaskRouteSnapshot.
//
// HeaderProfileStrategy must remain nil here. Its selected static headers are
// captured in HeaderPolicyCiphertext, while client-dynamic pass-through headers
// are rejected or handled separately at submission time.
type TaskRouteProviderSnapshot struct {
	Other string `json:"other,omitempty"`
	// ApiVersion freezes query- or provider-specific version overrides that are
	// distinct from Channel.Other (for example Azure's api-version query
	// parameter). Replay must prefer this value and only fall back to Other for
	// schema-6 rows created before this field was introduced.
	ApiVersion                        string                   `json:"api_version,omitempty"`
	StatusCodeMapping                 string                   `json:"status_code_mapping,omitempty"`
	OpenAIOrganization                string                   `json:"openai_organization,omitempty"`
	ChannelSettings                   dto.ChannelSettings      `json:"channel_settings"`
	ChannelOtherSettings              dto.ChannelOtherSettings `json:"channel_other_settings"`
	ResponsesBootstrapRecoveryEnabled bool                     `json:"responses_bootstrap_recovery_enabled,omitempty"`
}

// TaskRouteSnapshot is the persisted, credential-safe route identity for an
// asynchronous task. It intentionally contains no API key or header values;
// the concrete key is decrypted into TaskPrivateData.Key only in memory and
// is validated against the current channel before every follow-up request.
type TaskRouteSnapshot struct {
	SchemaVersion int `json:"schema_version,omitempty"`
	// Group is the concrete group selected at submission time. It is kept
	// separate from the route fingerprint because health state is scoped by
	// group as well as model/path/protocol.
	Group string `json:"group,omitempty"`
	// BaseURL is the exact channel endpoint selected at submission time. It is
	// intentionally persisted separately from the current Channel row so an
	// asynchronous task cannot be redirected by a later channel edit.
	BaseURL                  string `json:"base_url,omitempty"`
	RequestedModel           string `json:"requested_model,omitempty"`
	EffectiveUpstreamModel   string `json:"effective_upstream_model,omitempty"`
	Protocol                 string `json:"protocol,omitempty"`
	Operation                string `json:"operation,omitempty"`
	PollingOperation         string `json:"polling_operation,omitempty"`
	ContentOperation         string `json:"content_operation,omitempty"`
	SubmitPath               string `json:"submit_path,omitempty"`
	PollingPath              string `json:"polling_path,omitempty"`
	ContentPath              string `json:"content_path,omitempty"`
	RouteFingerprint         string `json:"route_fingerprint,omitempty"`
	PollingRouteFingerprint  string `json:"polling_route_fingerprint,omitempty"`
	ContentRouteFingerprint  string `json:"content_route_fingerprint,omitempty"`
	RouteScopeVersion        string `json:"route_scope_version,omitempty"`
	ChannelID                int    `json:"channel_id,omitempty"`
	ChannelType              int    `json:"channel_type,omitempty"`
	AdapterPlatform          string `json:"adapter_platform,omitempty"`
	AdapterIdentity          string `json:"adapter_identity,omitempty"`
	MultiKeyIndex            int    `json:"multi_key_index,omitempty"`
	CredentialScoped         bool   `json:"credential_scoped,omitempty"`
	HeaderProfileID          string `json:"header_profile_id,omitempty"`
	HeaderProfileVersion     string `json:"header_profile_version,omitempty"`
	HeaderProfileFingerprint string `json:"header_profile_fingerprint,omitempty"`
	// HeaderPolicyCiphertext contains the resolved static header map captured at
	// submission. It is encrypted with the application crypto secret and is
	// deliberately excluded from the snapshot fingerprint because its nonce is
	// randomized on every write.
	HeaderPolicyCaptured    bool   `json:"header_policy_captured,omitempty"`
	HeaderPolicyCiphertext  string `json:"header_policy_ciphertext,omitempty"`
	HeaderPolicyFingerprint string `json:"header_policy_fingerprint,omitempty"`
	// ProxyCaptured distinguishes a deliberately empty proxy from an old
	// snapshot that never captured proxy configuration. ProxyCiphertext uses
	// the same authenticated encryption key as task credentials and is never
	// stored in plaintext.
	ProxyCaptured    bool   `json:"proxy_captured,omitempty"`
	ProxyCiphertext  string `json:"proxy_ciphertext,omitempty"`
	ProxyFingerprint string `json:"proxy_fingerprint,omitempty"`
	Proxy            string `json:"-"`
	// ProviderRuntime is the complete non-secret provider configuration needed
	// by asynchronous follow-ups. It is mandatory for the current schema and is
	// included in SnapshotFingerprint so configuration tampering is detectable.
	ProviderRuntime     *TaskRouteProviderSnapshot `json:"provider_runtime,omitempty"`
	SnapshotFingerprint string                     `json:"snapshot_fingerprint,omitempty"`
	proxyDecoded        bool                       `json:"-"`
}

type taskRouteSnapshotFingerprintPayload struct {
	SchemaVersion            int                        `json:"schema_version"`
	Group                    string                     `json:"group"`
	BaseURL                  string                     `json:"base_url"`
	RequestedModel           string                     `json:"requested_model"`
	EffectiveUpstreamModel   string                     `json:"effective_upstream_model"`
	Protocol                 string                     `json:"protocol"`
	Operation                string                     `json:"operation"`
	PollingOperation         string                     `json:"polling_operation"`
	ContentOperation         string                     `json:"content_operation"`
	SubmitPath               string                     `json:"submit_path"`
	PollingPath              string                     `json:"polling_path"`
	ContentPath              string                     `json:"content_path"`
	RouteFingerprint         string                     `json:"route_fingerprint"`
	PollingFingerprint       string                     `json:"polling_route_fingerprint"`
	ContentFingerprint       string                     `json:"content_route_fingerprint"`
	RouteScopeVersion        string                     `json:"route_scope_version"`
	ChannelID                int                        `json:"channel_id"`
	ChannelType              int                        `json:"channel_type"`
	AdapterPlatform          string                     `json:"adapter_platform"`
	AdapterIdentity          string                     `json:"adapter_identity"`
	MultiKeyIndex            int                        `json:"multi_key_index"`
	CredentialScoped         bool                       `json:"credential_scoped"`
	HeaderProfileID          string                     `json:"header_profile_id"`
	HeaderProfileVersion     string                     `json:"header_profile_version"`
	HeaderProfileFingerprint string                     `json:"header_profile_fingerprint"`
	HeaderPolicyCaptured     bool                       `json:"header_policy_captured"`
	HeaderPolicyFingerprint  string                     `json:"header_policy_fingerprint"`
	ProxyCaptured            bool                       `json:"proxy_captured"`
	ProxyFingerprint         string                     `json:"proxy_fingerprint"`
	ProviderRuntime          *TaskRouteProviderSnapshot `json:"provider_runtime,omitempty"`
}

func (s TaskRouteSnapshot) fingerprintPayload() taskRouteSnapshotFingerprintPayload {
	return taskRouteSnapshotFingerprintPayload{
		SchemaVersion:            s.SchemaVersion,
		Group:                    strings.TrimSpace(s.Group),
		BaseURL:                  strings.TrimRight(strings.TrimSpace(s.BaseURL), "/"),
		RequestedModel:           strings.TrimSpace(s.RequestedModel),
		EffectiveUpstreamModel:   strings.TrimSpace(s.EffectiveUpstreamModel),
		Protocol:                 strings.TrimSpace(s.Protocol),
		Operation:                strings.TrimSpace(s.Operation),
		PollingOperation:         strings.TrimSpace(s.PollingOperation),
		ContentOperation:         strings.TrimSpace(s.ContentOperation),
		SubmitPath:               commonRelay.NormalizeRoutePath(s.SubmitPath),
		PollingPath:              commonRelay.NormalizeRoutePath(s.PollingPath),
		ContentPath:              commonRelay.NormalizeRoutePath(s.ContentPath),
		RouteFingerprint:         strings.TrimSpace(s.RouteFingerprint),
		PollingFingerprint:       strings.TrimSpace(s.PollingRouteFingerprint),
		ContentFingerprint:       strings.TrimSpace(s.ContentRouteFingerprint),
		RouteScopeVersion:        strings.TrimSpace(s.RouteScopeVersion),
		ChannelID:                s.ChannelID,
		ChannelType:              s.ChannelType,
		AdapterPlatform:          strings.TrimSpace(s.AdapterPlatform),
		AdapterIdentity:          strings.TrimSpace(s.AdapterIdentity),
		MultiKeyIndex:            s.MultiKeyIndex,
		CredentialScoped:         s.CredentialScoped,
		HeaderProfileID:          strings.TrimSpace(s.HeaderProfileID),
		HeaderProfileVersion:     strings.TrimSpace(s.HeaderProfileVersion),
		HeaderProfileFingerprint: strings.TrimSpace(s.HeaderProfileFingerprint),
		HeaderPolicyCaptured:     s.HeaderPolicyCaptured,
		HeaderPolicyFingerprint:  strings.TrimSpace(s.HeaderPolicyFingerprint),
		ProxyCaptured:            s.ProxyCaptured,
		ProxyFingerprint:         strings.TrimSpace(s.ProxyFingerprint),
		ProviderRuntime:          s.ProviderRuntime,
	}
}

// ComputeFingerprint returns the canonical integrity digest for the snapshot.
// Secret-bearing ciphertext and the in-memory proxy are intentionally excluded;
// the proxy is represented by ProxyFingerprint and authenticated separately.
func (s TaskRouteSnapshot) ComputeFingerprint() string {
	b, err := common.Marshal(s.fingerprintPayload())
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:])
}

// ComputeProxyFingerprint binds a captured proxy to the configured crypto
// secret without persisting the proxy or a directly brute-forceable plaintext
// hash.
func ComputeTaskRouteProxyFingerprint(proxyURL string) string {
	key := sha256.Sum256([]byte("new-api task route proxy fingerprint:" + common.CryptoSecret))
	h := hmac.New(sha256.New, key[:])
	_, _ = h.Write([]byte(strings.TrimSpace(proxyURL)))
	return hex.EncodeToString(h.Sum(nil))
}

// ComputeTaskRouteHeaderProfileFingerprint returns an authenticated digest of
// a resolved profile's headers. The digest is deliberately keyed so a task
// row cannot be used as a dictionary oracle for sensitive custom header
// values, while still detecting any profile-content change at replay time.
func ComputeTaskRouteHeaderProfileFingerprint(headers map[string]string) string {
	return computeTaskRouteHeaderFingerprint("new-api task route header profile fingerprint:", headers, false)
}

// ComputeTaskRouteHeaderPolicyFingerprint returns an authenticated digest of
// the complete static header map that was applied to an asynchronous task at
// submission. Unlike the profile-only fingerprint, this includes the merged
// channel/tag override and the selected user-agent value.
func ComputeTaskRouteHeaderPolicyFingerprint(headers map[string]string) string {
	return computeTaskRouteHeaderFingerprint("new-api task route header policy fingerprint:", headers, true)
}

func computeTaskRouteHeaderFingerprint(domain string, headers map[string]string, includeEmpty bool) string {
	normalized, err := normalizeTaskRouteHeaderPolicy(headers)
	if err != nil {
		return ""
	}
	if len(normalized) == 0 && !includeEmpty {
		return ""
	}
	type headerEntry struct {
		name  string
		value string
	}
	entries := make([]headerEntry, 0, len(normalized))
	for name, value := range normalized {
		entries = append(entries, headerEntry{name: strings.ToLower(name), value: value})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].name == entries[j].name {
			return entries[i].value < entries[j].value
		}
		return entries[i].name < entries[j].name
	})
	key := sha256.Sum256([]byte(domain + common.CryptoSecret))
	h := hmac.New(sha256.New, key[:])
	for _, entry := range entries {
		_, _ = h.Write([]byte(entry.name))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(entry.value))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

const taskRouteHeaderPolicyAAD = "new-api:task-route-header-policy:v1"

func taskRouteHeaderPolicyEncryptionKey() []byte {
	digest := sha256.Sum256([]byte("new-api task route header policy:" + common.CryptoSecret))
	return digest[:]
}

// EncryptTaskRouteHeaderPolicy protects the resolved static headers captured
// for an asynchronous task. Header values can contain provider credentials or
// signed tokens, so they must never be serialized as plaintext task metadata.
func EncryptTaskRouteHeaderPolicy(headers map[string]string) (string, error) {
	normalized, err := normalizeTaskRouteHeaderPolicy(headers)
	if err != nil {
		return "", err
	}
	plain, err := common.Marshal(normalized)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(taskRouteHeaderPolicyEncryptionKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, plain, []byte(taskRouteHeaderPolicyAAD))
	return taskPrivateKeyCipherPrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// DecryptTaskRouteHeaderPolicy decrypts the static header map captured in a
// task route snapshot and returns a defensive copy for request application.
func DecryptTaskRouteHeaderPolicy(ciphertext string) (map[string]string, error) {
	ciphertext = strings.TrimSpace(ciphertext)
	if ciphertext == "" {
		return nil, nil
	}
	if !strings.HasPrefix(ciphertext, taskPrivateKeyCipherPrefix) {
		return nil, errors.New("unsupported task route header policy ciphertext version")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(ciphertext, taskPrivateKeyCipherPrefix))
	if err != nil {
		return nil, fmt.Errorf("decode task route header policy ciphertext: %w", err)
	}
	block, err := aes.NewCipher(taskRouteHeaderPolicyEncryptionKey())
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize()+gcm.Overhead() {
		return nil, errors.New("task route header policy ciphertext is too short")
	}
	nonce := sealed[:gcm.NonceSize()]
	payload := sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, payload, []byte(taskRouteHeaderPolicyAAD))
	if err != nil {
		return nil, fmt.Errorf("decrypt task route header policy ciphertext: %w", err)
	}
	var headers map[string]string
	if err := common.Unmarshal(plain, &headers); err != nil {
		return nil, fmt.Errorf("decode task route header policy: %w", err)
	}
	normalized, err := normalizeTaskRouteHeaderPolicy(headers)
	if err != nil {
		return nil, fmt.Errorf("normalize task route header policy: %w", err)
	}
	return normalized, nil
}

func normalizeTaskRouteHeaderPolicy(headers map[string]string) (map[string]string, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	normalized := make(map[string]string, len(headers))
	seen := make(map[string]struct{}, len(headers))
	for name, value := range headers {
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" || value == "" {
			continue
		}
		if !dto.IsValidHeaderFieldName(name) {
			return nil, errors.New("task route header policy contains an invalid header name")
		}
		canonicalName := textproto.CanonicalMIMEHeaderKey(name)
		if canonicalName == "" {
			return nil, errors.New("task route header policy contains an invalid header name")
		}
		lookupName := strings.ToLower(canonicalName)
		if _, exists := seen[lookupName]; exists {
			return nil, errors.New("task route header policy contains duplicate header names")
		}
		seen[lookupName] = struct{}{}
		normalized[canonicalName] = value
	}
	if len(normalized) == 0 {
		return nil, nil
	}
	return normalized, nil
}

// ValidateHeaderPolicy checks the authenticated header policy introduced in
// schema 5. It intentionally does not require the provider-runtime fields
// introduced in schema 6, so a schema-5 row can be replayed without silently
// falling back to the mutable Header Profile configuration.
func (s *TaskRouteSnapshot) ValidateHeaderPolicy() error {
	if s == nil {
		return errors.New("task route snapshot is missing")
	}
	if s.SchemaVersion > TaskRouteSnapshotSchemaVersion {
		return fmt.Errorf("task route snapshot schema version %d is newer than supported version %d", s.SchemaVersion, TaskRouteSnapshotSchemaVersion)
	}
	if s.SchemaVersion < TaskRouteSnapshotHeaderPolicySchemaVersion {
		return nil
	}
	if !s.HeaderPolicyCaptured {
		return errors.New("task route snapshot header policy capture is incomplete")
	}
	if strings.TrimSpace(s.HeaderPolicyCiphertext) == "" || strings.TrimSpace(s.HeaderPolicyFingerprint) == "" {
		return errors.New("task route snapshot header policy capture is incomplete")
	}
	if strings.TrimSpace(s.HeaderProfileID) == "" && strings.TrimSpace(s.HeaderProfileFingerprint) != "" {
		return errors.New("task route snapshot header profile fingerprint has no profile identity")
	}
	if strings.TrimSpace(s.HeaderProfileID) != "" && strings.TrimSpace(s.HeaderProfileFingerprint) == "" {
		return errors.New("task route snapshot header profile fingerprint is missing")
	}
	capturedHeaders, err := DecryptTaskRouteHeaderPolicy(s.HeaderPolicyCiphertext)
	if err != nil {
		return fmt.Errorf("decrypt task route header policy: %w", err)
	}
	if ComputeTaskRouteHeaderPolicyFingerprint(capturedHeaders) != strings.TrimSpace(s.HeaderPolicyFingerprint) {
		return errors.New("task route snapshot header policy fingerprint mismatch")
	}
	return nil
}

// Validate checks the complete current-schema snapshot before it is used for a
// follow-up request. Schema-5 rows are validated for their authenticated
// header policy, while older snapshots retain their explicit compatibility
// path.
func (s *TaskRouteSnapshot) Validate() error {
	if s == nil {
		return errors.New("task route snapshot is missing")
	}
	if s.SchemaVersion > TaskRouteSnapshotSchemaVersion {
		return fmt.Errorf("task route snapshot schema version %d is newer than supported version %d", s.SchemaVersion, TaskRouteSnapshotSchemaVersion)
	}
	if s.SchemaVersion < TaskRouteSnapshotSchemaVersion {
		return s.ValidateHeaderPolicy()
	}
	if strings.TrimSpace(s.Group) == "" || strings.EqualFold(strings.TrimSpace(s.Group), "auto") || strings.TrimSpace(s.BaseURL) == "" ||
		strings.TrimSpace(s.RequestedModel) == "" || strings.TrimSpace(s.EffectiveUpstreamModel) == "" ||
		strings.TrimSpace(s.Protocol) == "" || strings.TrimSpace(s.Operation) == "" ||
		strings.TrimSpace(s.PollingOperation) == "" || strings.TrimSpace(s.ContentOperation) == "" ||
		(commonRelay.NormalizeRoutePath(s.SubmitPath) == "/" && s.ChannelType != constant.ChannelTypeJimeng) ||
		(commonRelay.NormalizeRoutePath(s.PollingPath) == "/" && s.ChannelType != constant.ChannelTypeJimeng) ||
		commonRelay.NormalizeRoutePath(s.ContentPath) == "/" {
		return errors.New("task route snapshot required identity fields are incomplete; group must be concrete")
	}
	if strings.TrimSpace(s.RouteFingerprint) == "" || strings.TrimSpace(s.PollingRouteFingerprint) == "" || strings.TrimSpace(s.ContentRouteFingerprint) == "" {
		return errors.New("task route snapshot phase fingerprint is missing")
	}
	if s.ChannelID <= 0 || !constant.IsValidChannelType(s.ChannelType) {
		return errors.New("task route snapshot channel identity is invalid")
	}
	if s.ProviderRuntime == nil {
		return errors.New("task route snapshot provider runtime is incomplete")
	}
	if strings.TrimSpace(s.ProviderRuntime.ChannelSettings.Proxy) != "" {
		return errors.New("task route snapshot provider runtime must not contain plaintext proxy")
	}
	if s.ProviderRuntime.ChannelOtherSettings.HeaderProfileStrategy != nil {
		return errors.New("task route snapshot provider runtime must not contain header profile headers")
	}
	if strings.TrimSpace(s.AdapterPlatform) == "" || strings.TrimSpace(s.AdapterIdentity) == "" || strings.TrimSpace(s.RouteScopeVersion) == "" {
		return errors.New("task route snapshot adapter or scope identity is incomplete")
	}
	if !s.ProxyCaptured || strings.TrimSpace(s.ProxyCiphertext) == "" || strings.TrimSpace(s.ProxyFingerprint) == "" {
		return errors.New("task route snapshot proxy identity is incomplete")
	}
	proxy, err := DecryptTaskRouteProxy(s.ProxyCiphertext)
	if err != nil {
		return fmt.Errorf("decrypt task route proxy: %w", err)
	}
	if ComputeTaskRouteProxyFingerprint(proxy) != strings.TrimSpace(s.ProxyFingerprint) {
		return errors.New("task route snapshot proxy fingerprint mismatch")
	}
	if s.proxyDecoded && strings.TrimSpace(s.Proxy) != strings.TrimSpace(proxy) {
		return errors.New("task route snapshot proxy plaintext mismatch")
	}
	if !s.proxyDecoded && strings.TrimSpace(s.Proxy) != "" && strings.TrimSpace(s.Proxy) != strings.TrimSpace(proxy) {
		return errors.New("task route snapshot proxy plaintext mismatch")
	}
	if err := s.ValidateHeaderPolicy(); err != nil {
		return err
	}
	if strings.TrimSpace(s.SnapshotFingerprint) == "" {
		return errors.New("task route snapshot fingerprint is missing")
	}
	if got := s.ComputeFingerprint(); got == "" || got != strings.TrimSpace(s.SnapshotFingerprint) {
		return errors.New("task route snapshot fingerprint mismatch")
	}
	if s.proxyDecoded && ComputeTaskRouteProxyFingerprint(s.Proxy) != strings.TrimSpace(s.ProxyFingerprint) {
		return errors.New("task route snapshot proxy fingerprint mismatch")
	}
	return nil
}

// TaskBillingContext 记录任务提交时的计费参数，以便轮询阶段可以重新计算额度。
type TaskBillingContext struct {
	ModelPrice      float64            `json:"model_price,omitempty"`       // 模型单价
	GroupRatio      float64            `json:"group_ratio,omitempty"`       // 分组倍率
	ModelRatio      float64            `json:"model_ratio,omitempty"`       // 模型倍率
	OtherRatios     map[string]float64 `json:"other_ratios,omitempty"`      // 附加倍率（时长、分辨率等）
	OriginModelName string             `json:"origin_model_name,omitempty"` // 模型名称，必须为OriginModelName
	PerCallBilling  bool               `json:"per_call_billing,omitempty"`  // 按次计费：跳过轮询阶段的差额结算
}

// GetUpstreamTaskID 获取上游真实 task ID（用于与 provider 通信）
// 旧数据没有 UpstreamTaskID 时，TaskID 本身就是上游 ID
func (t *Task) GetUpstreamTaskID() string {
	if t.PrivateData.UpstreamTaskID != "" {
		return t.PrivateData.UpstreamTaskID
	}
	return t.TaskID
}

// isValidTaskResultURL accepts only absolute result URLs that can be safely
// replayed by task consumers. Older task rows stored the result URL in
// FailReason, so this validation is intentionally narrow to avoid exposing an
// ordinary failure message as a downloadable URL.
func isValidTaskResultURL(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return parsed.Host != ""
	case "data":
		return isValidTaskDataResultURL(parsed.Opaque)
	default:
		return false
	}
}

func isValidTaskDataResultURL(opaque string) bool {
	parts := strings.SplitN(opaque, ",", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return false
	}
	metadata := strings.Split(parts[0], ";")
	mimeType := strings.ToLower(strings.TrimSpace(metadata[0]))
	if !strings.HasPrefix(mimeType, "video/") && mimeType != "application/octet-stream" {
		return false
	}
	base64Payload := false
	for _, parameter := range metadata[1:] {
		if strings.EqualFold(strings.TrimSpace(parameter), "base64") {
			if base64Payload {
				return false
			}
			base64Payload = true
		}
	}
	return base64Payload && !strings.ContainsAny(parts[1], "\r\n\t ")
}

// GetResultURL 获取任务结果 URL（视频地址等）。新数据存在
// PrivateData.ResultURL 中；旧数据曾将结果地址写入 FailReason，只有在
// 其确实是合法绝对结果 URL 时才兼容回读，普通失败文案继续返回空字符串。
func (t *Task) GetResultURL() string {
	if t == nil {
		return ""
	}
	if resultURL := strings.TrimSpace(t.PrivateData.ResultURL); resultURL != "" {
		return resultURL
	}
	if resultURL := strings.TrimSpace(t.FailReason); isValidTaskResultURL(resultURL) {
		return resultURL
	}
	return ""
}

// GenerateTaskID 生成对外暴露的 task_xxxx 格式 ID
func GenerateTaskID() string {
	key, _ := common.GenerateRandomCharsKey(32)
	return "task_" + key
}

func (p *TaskPrivateData) Scan(val interface{}) error {
	if p == nil {
		return errors.New("task private data scan target is nil")
	}
	var bytesValue []byte
	switch value := val.(type) {
	case nil:
		*p = TaskPrivateData{}
		return nil
	case []byte:
		bytesValue = value
	case string:
		bytesValue = []byte(value)
	default:
		return fmt.Errorf("unsupported task private data value type %T", val)
	}
	if len(bytesValue) == 0 {
		*p = TaskPrivateData{}
		return nil
	}
	var stored TaskPrivateData
	if err := common.Unmarshal(bytesValue, &stored); err != nil {
		return err
	}
	if strings.TrimSpace(stored.KeyCiphertext) != "" {
		key, err := decryptTaskPrivateKey(stored.KeyCiphertext)
		if err != nil {
			return err
		}
		stored.Key = key
	}
	if stored.RouteSnapshot != nil {
		snapshot := *stored.RouteSnapshot
		if snapshot.ProxyCaptured && strings.TrimSpace(snapshot.ProxyCiphertext) != "" {
			proxy, err := DecryptTaskRouteProxy(snapshot.ProxyCiphertext)
			if err != nil {
				return err
			}
			snapshot.Proxy = proxy
		}
		if snapshot.ProxyCaptured {
			snapshot.proxyDecoded = true
		}
		// Current-schema snapshots are authenticated route state, not optional
		// metadata. Validate them even when ProxyCaptured is false so a malformed
		// row cannot be loaded and later bypass the strict follow-up guards.
		if snapshot.SchemaVersion >= TaskRouteSnapshotHeaderPolicySchemaVersion || snapshot.ProxyCaptured {
			if err := snapshot.Validate(); err != nil {
				return err
			}
		}
		stored.RouteSnapshot = &snapshot
	}
	*p = stored
	return nil
}

func (p TaskPrivateData) Value() (driver.Value, error) {
	if (p == TaskPrivateData{}) {
		return nil, nil
	}
	stored := p
	if stored.Key != "" {
		ciphertext, err := encryptTaskPrivateKey(stored.Key)
		if err != nil {
			return nil, err
		}
		stored.Key = ""
		stored.KeyCiphertext = ciphertext
	}
	if stored.RouteSnapshot != nil {
		snapshot := *stored.RouteSnapshot
		if snapshot.ProxyCaptured || snapshot.Proxy != "" {
			snapshot.ProxyCaptured = true
			proxyKnown := snapshot.proxyDecoded || strings.TrimSpace(snapshot.Proxy) != ""
			if !proxyKnown && strings.TrimSpace(snapshot.ProxyCiphertext) != "" {
				// A task loaded through a lightweight path may carry only the
				// persisted ciphertext. Preserve it until the proxy is explicitly
				// resolved; treating the missing in-memory plaintext as an empty
				// proxy would silently discard the submission route.
			} else {
				ciphertext, err := EncryptTaskRouteProxy(snapshot.Proxy)
				if err != nil {
					return nil, err
				}
				snapshot.ProxyCiphertext = ciphertext
				snapshot.ProxyFingerprint = ComputeTaskRouteProxyFingerprint(snapshot.Proxy)
			}
			snapshot.Proxy = ""
			if snapshot.SchemaVersion >= TaskRouteSnapshotSchemaVersion {
				snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()
			}
		}
		stored.RouteSnapshot = &snapshot
	}
	return common.Marshal(stored)
}

const taskPrivateKeyCipherPrefix = "v1:"
const taskRouteProxyCipherPrefix = "v2:"

var taskPrivateKeyAAD = []byte("new-api:task-private-key:v1")
var taskRouteProxyAAD = []byte("new-api:task-route-proxy:v2")

func taskPrivateKeyEncryptionKey() []byte {
	digest := sha256.Sum256([]byte("new-api task private key:" + common.CryptoSecret))
	return digest[:]
}

func taskRouteProxyEncryptionKey() []byte {
	digest := sha256.Sum256([]byte("new-api task route proxy:" + common.CryptoSecret))
	return digest[:]
}

func encryptTaskPrivateKey(key string) (string, error) {
	if key == "" {
		return "", nil
	}
	block, err := aes.NewCipher(taskPrivateKeyEncryptionKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(key), taskPrivateKeyAAD)
	return taskPrivateKeyCipherPrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func decryptTaskPrivateKey(ciphertext string) (string, error) {
	ciphertext = strings.TrimSpace(ciphertext)
	if ciphertext == "" {
		return "", nil
	}
	if !strings.HasPrefix(ciphertext, taskPrivateKeyCipherPrefix) {
		return "", errors.New("unsupported task private key ciphertext version")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(ciphertext, taskPrivateKeyCipherPrefix))
	if err != nil {
		return "", fmt.Errorf("decode task private key ciphertext: %w", err)
	}
	block, err := aes.NewCipher(taskPrivateKeyEncryptionKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize()+gcm.Overhead() {
		return "", errors.New("task private key ciphertext is too short")
	}
	nonce := sealed[:gcm.NonceSize()]
	payload := sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, payload, taskPrivateKeyAAD)
	if err != nil {
		return "", fmt.Errorf("decrypt task private key ciphertext: %w", err)
	}
	return string(plain), nil
}

// EncryptTaskRouteProxy encrypts a channel proxy before it is placed in a
// persisted task route snapshot. The proxy may contain credentials and must
// never be serialized in plaintext.
func EncryptTaskRouteProxy(proxyURL string) (string, error) {
	// An empty proxy is a meaningful captured value: it prevents a later
	// channel edit from reintroducing a proxy into an asynchronous follow-up.
	// Unlike a missing private key, it must therefore still have an authenticated
	// ciphertext representation.
	block, err := aes.NewCipher(taskRouteProxyEncryptionKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(proxyURL), taskRouteProxyAAD)
	return taskRouteProxyCipherPrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// DecryptTaskRouteProxy decrypts the proxy captured in a task route snapshot.
func DecryptTaskRouteProxy(ciphertext string) (string, error) {
	ciphertext = strings.TrimSpace(ciphertext)
	if ciphertext == "" {
		return "", nil
	}
	if strings.HasPrefix(ciphertext, taskRouteProxyCipherPrefix) {
		return decryptTaskCiphertext(ciphertext, taskRouteProxyCipherPrefix, taskRouteProxyEncryptionKey(), taskRouteProxyAAD, "task route proxy")
	}
	// v1 route snapshots used the task-private-key domain. Keep this narrow
	// compatibility path for rows written before the ciphertext domains were
	// separated; all new writes use v2 above.
	if strings.HasPrefix(ciphertext, taskPrivateKeyCipherPrefix) {
		return decryptTaskCiphertext(ciphertext, taskPrivateKeyCipherPrefix, taskPrivateKeyEncryptionKey(), taskPrivateKeyAAD, "legacy task route proxy")
	}
	return "", errors.New("unsupported task route proxy ciphertext version")
}

func decryptTaskCiphertext(ciphertext, prefix string, key, aad []byte, label string) (string, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(ciphertext, prefix))
	if err != nil {
		return "", fmt.Errorf("decode %s ciphertext: %w", label, err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize()+gcm.Overhead() {
		return "", fmt.Errorf("%s ciphertext is too short", label)
	}
	nonce := sealed[:gcm.NonceSize()]
	payload := sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, payload, aad)
	if err != nil {
		return "", fmt.Errorf("decrypt %s ciphertext: %w", label, err)
	}
	return string(plain), nil
}

// SyncTaskQueryParams 用于包含所有搜索条件的结构体，可以根据需求添加更多字段
type SyncTaskQueryParams struct {
	Platform       constant.TaskPlatform
	ChannelID      string
	TaskID         string
	UserID         string
	Action         string
	Status         string
	StartTimestamp int64
	EndTimestamp   int64
	UserIDs        []int
}

func InitTask(platform constant.TaskPlatform, relayInfo *commonRelay.RelayInfo) *Task {
	properties := Properties{}
	privateData := TaskPrivateData{}
	channelID := 0
	if relayInfo != nil && relayInfo.ChannelMeta != nil {
		channelID = relayInfo.ChannelMeta.ChannelId
		// Persist the concrete credential selected for this asynchronous task.
		// Polling must use the same key as submission; channel.Key may represent
		// an aggregate multi-key value and can route a task to another account.
		privateData.Key = relayInfo.ChannelMeta.ApiKey
		if relayInfo.UpstreamModelName != "" {
			properties.UpstreamModelName = relayInfo.UpstreamModelName
		}
		if relayInfo.OriginModelName != "" {
			properties.OriginModelName = relayInfo.OriginModelName
		}
	}
	if relayInfo != nil && relayInfo.ChannelMeta == nil && relayInfo.ChannelId > 0 {
		// ChannelMeta is the authoritative attempt-scoped identity whenever it is
		// present. Only use the legacy RelayInfo field when a compatibility caller
		// has no attempt metadata at all; otherwise a stale retry value can make the
		// durable task channel disagree with its route snapshot.
		channelID = relayInfo.ChannelId
	}

	// 使用预生成的公开 ID（如果有），否则新生成
	taskID := ""
	if relayInfo.TaskRelayInfo != nil && relayInfo.TaskRelayInfo.PublicTaskID != "" {
		taskID = relayInfo.TaskRelayInfo.PublicTaskID
	} else {
		taskID = GenerateTaskID()
	}

	t := &Task{
		TaskID:      taskID,
		UserId:      relayInfo.UserId,
		Group:       relayInfo.UsingGroup,
		SubmitTime:  time.Now().Unix(),
		Status:      TaskStatusNotStart,
		Progress:    "0%",
		ChannelId:   channelID,
		Platform:    platform,
		Properties:  properties,
		PrivateData: privateData,
	}
	return t
}

func TaskGetAllUserTask(userId int, startIdx int, num int, queryParams SyncTaskQueryParams) []*Task {
	tasks, _ := TaskGetAllUserTaskWithError(userId, startIdx, num, queryParams)
	return tasks
}

// TaskGetAllUserTaskWithError returns user-scoped tasks together with any
// database or private-data decoding error. Callers serving an API response
// must not turn a malformed route snapshot into an empty successful page.
func TaskGetAllUserTaskWithError(userId int, startIdx int, num int, queryParams SyncTaskQueryParams) ([]*Task, error) {
	var tasks []*Task

	// 初始化查询构建器
	query := DB.Where("user_id = ?", userId)

	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.StartTimestamp != 0 {
		// 假设您已将前端传来的时间戳转换为数据库所需的时间格式，并处理了时间戳的验证和解析
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err := query.Omit("channel_id").Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil, err
	}

	return tasks, nil
}

func TaskGetAllTasks(startIdx int, num int, queryParams SyncTaskQueryParams) []*Task {
	tasks, _ := TaskGetAllTasksWithError(startIdx, num, queryParams)
	return tasks
}

// TaskGetAllTasksWithError returns administrator-visible tasks together with
// any database or private-data decoding error. Returning the error keeps
// route snapshot corruption visible to the caller instead of reporting an
// empty page as a successful response.
func TaskGetAllTasksWithError(startIdx int, num int, queryParams SyncTaskQueryParams) ([]*Task, error) {
	var tasks []*Task

	// 初始化查询构建器
	query := DB

	// 添加过滤条件
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.UserID != "" {
		query = query.Where("user_id = ?", queryParams.UserID)
	}
	if len(queryParams.UserIDs) != 0 {
		query = query.Where("user_id in (?)", queryParams.UserIDs)
	}
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err := query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil, err
	}

	return tasks, nil
}

func GetTimedOutUnfinishedTasks(cutoffUnix int64, limit int) []*Task {
	tasks, _ := GetTimedOutUnfinishedTasksWithError(cutoffUnix, limit)
	return tasks
}

// GetTimedOutUnfinishedTasksWithError returns unfinished tasks together with
// any database or private-data decoding error. A malformed current-schema
// route snapshot must not be mistaken for an empty result by background
// polling callers.
func GetTimedOutUnfinishedTasksWithError(cutoffUnix int64, limit int) ([]*Task, error) {
	query := backgroundDB().Model(&Task{}).Where("progress != ?", "100%").
		Where("COALESCE(status, '') NOT IN ?", []string{TaskStatusFailure, TaskStatusSuccess}).
		Where("submit_time < ?", cutoffUnix).
		Order("submit_time")
	return loadTasksWithPrivateDataIsolation(query, limit)
}

func GetAllUnFinishSyncTasks(limit int) []*Task {
	tasks, _ := GetAllUnFinishSyncTasksWithError(limit)
	return tasks
}

// GetAllUnFinishSyncTasksWithError returns unfinished synchronous-provider
// tasks together with any database or private-data decoding error. Callers
// that drive a polling loop should handle the error explicitly and leave the
// rows pending for a later retry instead of treating the batch as empty.
func GetAllUnFinishSyncTasksWithError(limit int) ([]*Task, error) {
	// get all tasks progress is not 100%
	query := backgroundDB().Model(&Task{}).Where("progress != ?", "100%").
		Where("COALESCE(status, '') NOT IN ?", []string{TaskStatusFailure, TaskStatusSuccess}).
		Order("id")
	return loadTasksWithPrivateDataIsolation(query, limit)
}

// loadTasksWithPrivateDataIsolation reads private_data as raw bytes in a small
// page, loads the remaining columns without the custom scanner, and decodes
// each payload independently. A corrupt private-data value is reported while
// healthy tasks in the same polling window remain available for processing.
func loadTasksWithPrivateDataIsolation(query *gorm.DB, limit int) ([]*Task, error) {
	if query == nil {
		return nil, errors.New("task query is nil")
	}
	if limit <= 0 {
		limit = 100
	}
	tasks := make([]*Task, 0, limit)
	var scanErrors []error
	offset := 0
	for len(tasks) < limit {
		batchLimit := limit - len(tasks)
		var privateRows []taskPrivateDataRow
		if err := query.Session(&gorm.Session{}).Select("id", "private_data").Offset(offset).Limit(batchLimit).Find(&privateRows).Error; err != nil {
			return tasks, errors.Join(append(scanErrors, err)...)
		}
		if len(privateRows) == 0 {
			break
		}
		offset += len(privateRows)
		var pageTasks []*Task
		if err := query.Session(&gorm.Session{}).Omit("PrivateData").Offset(offset - len(privateRows)).Limit(batchLimit).Find(&pageTasks).Error; err != nil {
			return tasks, errors.Join(append(scanErrors, err)...)
		}
		privateByID := make(map[int64][]byte, len(privateRows))
		for _, row := range privateRows {
			privateByID[row.ID] = row.PrivateData
		}
		for _, task := range pageTasks {
			raw, ok := privateByID[task.ID]
			if !ok {
				scanErrors = append(scanErrors, fmt.Errorf("scan task %d: private data row disappeared during query", task.ID))
				continue
			}
			var privateData TaskPrivateData
			if err := privateData.Scan(raw); err != nil {
				scanErrors = append(scanErrors, fmt.Errorf("scan task %d: %w", task.ID, err))
				continue
			}
			task.PrivateData = privateData
			tasks = append(tasks, task)
			if len(tasks) == limit {
				break
			}
		}
	}
	return tasks, errors.Join(scanErrors...)
}

type taskPrivateDataRow struct {
	ID          int64  `gorm:"column:id"`
	PrivateData []byte `gorm:"column:private_data"`
}

// TaskCountUnfinished returns the number of persisted asynchronous tasks that
// have not reached a terminal state. It is used for bounded administrator
// observability and never changes task lifecycle state.
func TaskCountUnfinished() int64 {
	count, err := TaskCountUnfinishedWithError()
	if err != nil {
		common.SysError(fmt.Sprintf("failed to count unfinished tasks: %v", err))
		return 0
	}
	return count
}

// TaskCountUnfinishedWithError is the error-preserving form used by callers
// that need to distinguish an empty queue from a database failure.
func TaskCountUnfinishedWithError() (int64, error) {
	var count int64
	if DB == nil {
		return 0, errors.New("database is not initialized")
	}
	err := backgroundDB().Model(&Task{}).Where("progress != ?", "100%").
		Where("COALESCE(status, '') NOT IN ?", []TaskStatus{TaskStatusFailure, TaskStatusSuccess}).
		Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

func GetByOnlyTaskId(taskId string) (*Task, bool, error) {
	if taskId == "" {
		return nil, false, nil
	}
	var task *Task
	var err error
	err = DB.Where("task_id = ?", taskId).First(&task).Error
	exist, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	return task, exist, err
}

func GetByTaskId(userId int, taskId string) (*Task, bool, error) {
	if taskId == "" {
		return nil, false, nil
	}
	var task *Task
	var err error
	err = DB.Where("user_id = ? and task_id = ?", userId, taskId).
		First(&task).Error
	exist, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	return task, exist, err
}

func GetByTaskIds(userId int, taskIds []any) ([]*Task, error) {
	if len(taskIds) == 0 {
		return nil, nil
	}
	var task []*Task
	var err error
	err = DB.Where("user_id = ? and task_id in (?)", userId, taskIds).
		Find(&task).Error
	if err != nil {
		return nil, err
	}
	return task, nil
}

// validateTaskRouteSnapshotForWrite is the durable write gate shared by all
// asynchronous task tables. Legacy rows without a current-schema snapshot
// remain readable, while a new snapshot must be complete and must belong to
// the same channel row that owns the task. Keeping this gate on every row-level
// write prevents an in-memory mutation from bypassing the integrity checks
// performed when task private data is loaded.
func validateTaskRouteSnapshotForWrite(snapshot *TaskRouteSnapshot, channelID int) error {
	if snapshot == nil || snapshot.SchemaVersion < TaskRouteSnapshotHeaderPolicySchemaVersion {
		return nil
	}
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("task route snapshot validation failed: %w", err)
	}
	if channelID <= 0 || snapshot.ChannelID != channelID {
		return fmt.Errorf("task route snapshot channel mismatch: snapshot=%d task=%d", snapshot.ChannelID, channelID)
	}
	return nil
}

func (Task *Task) Insert() error {
	if Task == nil {
		return errors.New("task is nil")
	}
	if err := validateTaskRouteSnapshotForWrite(Task.PrivateData.RouteSnapshot, Task.ChannelId); err != nil {
		return err
	}
	var err error
	err = DB.Create(Task).Error
	return err
}

type taskSnapshot struct {
	Status     TaskStatus
	Progress   string
	StartTime  int64
	FinishTime int64
	FailReason string
	ResultURL  string
	Data       json.RawMessage
}

func (s taskSnapshot) Equal(other taskSnapshot) bool {
	return s.Status == other.Status &&
		s.Progress == other.Progress &&
		s.StartTime == other.StartTime &&
		s.FinishTime == other.FinishTime &&
		s.FailReason == other.FailReason &&
		s.ResultURL == other.ResultURL &&
		bytes.Equal(s.Data, other.Data)
}

func (t *Task) Snapshot() taskSnapshot {
	return taskSnapshot{
		Status:     t.Status,
		Progress:   t.Progress,
		StartTime:  t.StartTime,
		FinishTime: t.FinishTime,
		FailReason: t.FailReason,
		ResultURL:  t.PrivateData.ResultURL,
		Data:       t.Data,
	}
}

func (Task *Task) Update() error {
	if Task == nil {
		return errors.New("task is nil")
	}
	if err := validateTaskRouteSnapshotForWrite(Task.PrivateData.RouteSnapshot, Task.ChannelId); err != nil {
		return err
	}
	return DB.Save(Task).Error
}

// UpdateWithStatus performs a conditional UPDATE guarded by fromStatus (CAS).
// Returns (true, nil) if this caller won the update, (false, nil) if
// another process already moved the task out of fromStatus.
//
// Uses Model().Select("*").Updates() instead of Save() because GORM's Save
// falls back to INSERT ON CONFLICT when the WHERE-guarded UPDATE matches
// zero rows, which silently bypasses the CAS guard.
func (t *Task) UpdateWithStatus(fromStatus TaskStatus) (bool, error) {
	if t == nil {
		return false, errors.New("task is nil")
	}
	if err := validateTaskRouteSnapshotForWrite(t.PrivateData.RouteSnapshot, t.ChannelId); err != nil {
		return false, err
	}
	result := DB.Model(t).Where("status = ?", fromStatus).Select("*").Updates(t)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

var allowedTaskBulkUpdateFields = map[string]struct{}{
	"progress":    {},
	"start_time":  {},
	"finish_time": {},
	"fail_reason": {},
	"properties":  {},
	"data":        {},
}

func validateTaskBulkUpdateParams(params map[string]any) (map[string]any, error) {
	if len(params) == 0 {
		return nil, nil
	}
	normalized := make(map[string]any, len(params))
	for field, value := range params {
		field = strings.TrimSpace(field)
		if _, ok := allowedTaskBulkUpdateFields[field]; !ok {
			return nil, fmt.Errorf("task bulk update field %q is not allowed", field)
		}
		normalized[field] = value
	}
	return normalized, nil
}

func normalizeTaskIDs(taskIDs []string) []string {
	if len(taskIDs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(taskIDs))
	normalized := make([]string, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		taskID = strings.TrimSpace(taskID)
		if taskID == "" {
			continue
		}
		if _, ok := seen[taskID]; ok {
			continue
		}
		seen[taskID] = struct{}{}
		normalized = append(normalized, taskID)
	}
	return normalized
}

func normalizeTaskDatabaseIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(ids))
	normalized := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	return normalized
}

func updateTaskBulk(where func(*gorm.DB) *gorm.DB, params map[string]any) error {
	normalized, err := validateTaskBulkUpdateParams(params)
	if err != nil {
		return err
	}
	if len(normalized) == 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var tasks []*Task
		query := where(tx)
		if !common.UsingSQLite {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Find(&tasks).Error; err != nil {
			return fmt.Errorf("load tasks before bulk update: %w", err)
		}
		for _, task := range tasks {
			if task == nil {
				return errors.New("load tasks before bulk update: task is nil")
			}
			if err := validateTaskRouteSnapshotForWrite(task.PrivateData.RouteSnapshot, task.ChannelId); err != nil {
				return fmt.Errorf("validate task %d before bulk update: %w", task.ID, err)
			}
		}
		if err := where(tx).Updates(normalized).Error; err != nil {
			return fmt.Errorf("bulk update tasks: %w", err)
		}
		return nil
	})
}

// TaskBulkUpdate performs a non-lifecycle bulk UPDATE by upstream task ID.
// All matched rows are loaded and their route snapshots are validated before
// any write. Status, identity, billing, credential, and private-data fields
// are intentionally rejected; lifecycle transitions must use UpdateWithStatus.
func TaskBulkUpdate(taskIds []string, params map[string]any) error {
	taskIds = normalizeTaskIDs(taskIds)
	if len(taskIds) == 0 {
		return nil
	}
	return updateTaskBulk(func(tx *gorm.DB) *gorm.DB {
		return tx.Model(&Task{}).Where("task_id IN ?", taskIds)
	}, params)
}

// TaskBulkUpdateByID performs a non-lifecycle bulk UPDATE by primary key ID.
// It shares the same snapshot gate and field whitelist as TaskBulkUpdate.
func TaskBulkUpdateByID(ids []int64, params map[string]any) error {
	ids = normalizeTaskDatabaseIDs(ids)
	if len(ids) == 0 {
		return nil
	}
	return updateTaskBulk(func(tx *gorm.DB) *gorm.DB {
		return tx.Model(&Task{}).Where("id IN ?", ids)
	}, params)
}

type TaskQuotaUsage struct {
	Mode  string  `json:"mode"`
	Count float64 `json:"count"`
}

// TaskCountAllTasks returns total tasks that match the given query params (admin usage)
func TaskCountAllTasks(queryParams SyncTaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Task{})
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.UserID != "" {
		query = query.Where("user_id = ?", queryParams.UserID)
	}
	if len(queryParams.UserIDs) != 0 {
		query = query.Where("user_id in (?)", queryParams.UserIDs)
	}
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

// TaskCountAllUserTask returns total tasks for given user
func TaskCountAllUserTask(userId int, queryParams SyncTaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Task{}).Where("user_id = ?", userId)
	if queryParams.TaskID != "" {
		query = query.Where("task_id = ?", queryParams.TaskID)
	}
	if queryParams.Action != "" {
		query = query.Where("action = ?", queryParams.Action)
	}
	if queryParams.Status != "" {
		query = query.Where("status = ?", queryParams.Status)
	}
	if queryParams.Platform != "" {
		query = query.Where("platform = ?", queryParams.Platform)
	}
	if queryParams.StartTimestamp != 0 {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != 0 {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}
func (t *Task) ToOpenAIVideo() *dto.OpenAIVideo {
	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = t.TaskID
	openAIVideo.Status = t.Status.ToVideoStatus()
	openAIVideo.Model = t.Properties.OriginModelName
	openAIVideo.SetProgressStr(t.Progress)
	openAIVideo.CreatedAt = t.CreatedAt
	openAIVideo.CompletedAt = t.UpdatedAt
	openAIVideo.SetMetadata("url", t.GetResultURL())
	return openAIVideo
}
