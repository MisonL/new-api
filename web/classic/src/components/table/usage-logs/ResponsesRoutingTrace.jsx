/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React from 'react';
import { Space, Tag, Tooltip } from '@douyinfe/semi-ui';
import {
  getResponsesRoutingChannelTrace,
  getResponsesRoutingEventDisplay,
  getResponsesRoutingFinalPath,
  getResponsesRoutingResult,
  getResponsesRoutingResultDisplay,
  getResponsesRoutingTrace,
  hasResponsesRoutingChannelTrace,
} from '../../../helpers/usageLogsResponsesCompact';
import {
  getResponsesRoutingTraceStageDisplay,
  getResponsesRoutingTraceTone,
} from '../../../helpers/usageLogsResponsesRoutingTrace';

const traceStages = [
  'request_preparation',
  'route_selection',
  'upstream_attempt',
  'recovery',
];

const codeLikeEvents = new Set([
  'channel_skip',
  'compact_channel_skip',
  'compact_route_decision',
  'compact_summary_model',
  'compact_summary_models',
  'previous_id_action',
  'reasoning_context_action',
]);

function getTraceGroups(other) {
  const trace = getResponsesRoutingTrace(other);
  return traceStages
    .map((stage) => ({
      stage,
      items: trace.filter((item) => item.stage === stage),
    }))
    .filter((group) => group.items.length > 0);
}

function formatChannelLabel(channelId, t) {
  return t('渠道 #{{channel}}', { channel: channelId });
}

function formatChannelAttempt(attempt, t) {
  const details = [];
  if (attempt.outcome === 'success') {
    details.push(t('成功'));
  } else if (attempt.outcome === 'error') {
    details.push(t('错误'));
  } else {
    details.push(t('未知'));
  }
  if (attempt.status_code !== undefined) {
    details.push(t('状态码：{{status}}', { status: attempt.status_code }));
  }
  const error = [attempt.error_code, attempt.error_category]
    .filter(Boolean)
    .join(' / ');
  if (error) {
    details.push(t('错误：{{error}}', { error }));
  }
  return t('{{channel}}：{{details}}', {
    channel: formatChannelLabel(attempt.channel_id, t),
    details: details.join(t('；')),
  });
}

function RoutingOverview({ other, t }) {
  const result = getResponsesRoutingResult(other);
  if (!result) {
    return null;
  }

  const resultDisplay = getResponsesRoutingResultDisplay(result, t);
  const finalPath = getResponsesRoutingFinalPath(other);
  return (
    <div className='usage-log-routing-overview'>
      <div className='usage-log-routing-overview-item'>
        <span className='usage-log-routing-overview-label'>
          {t('最终结果')}
        </span>
        <span
          className={`usage-log-routing-overview-value is-${resultDisplay.color}`}
        >
          {resultDisplay.label}
        </span>
      </div>
      <div className='usage-log-routing-overview-item'>
        <span className='usage-log-routing-overview-label'>
          {t('已记录动作')}
        </span>
        <span className='usage-log-routing-overview-value is-count'>
          {result.actionCount}
        </span>
      </div>
      <div className='usage-log-routing-overview-item is-path'>
        <span className='usage-log-routing-overview-label'>
          {t('最终路径')}
        </span>
        {finalPath ? (
          <code className='usage-log-routing-overview-path'>{finalPath}</code>
        ) : (
          <span className='usage-log-routing-overview-unrecorded'>
            {t('未记录')}
          </span>
        )}
      </div>
    </div>
  );
}

export function ResponsesRoutingTrace({ other, t, compact = false }) {
  const traceGroups = getTraceGroups(other);
  const channelTrace = getResponsesRoutingChannelTrace(other);
  const showChannelTrace = hasResponsesRoutingChannelTrace(channelTrace);
  return (
    <div
      className={['usage-log-routing-trace', compact ? 'is-compact' : '']
        .filter(Boolean)
        .join(' ')}
    >
      <div className='usage-log-routing-trace-header'>
        <span className='usage-log-routing-trace-title'>{t('执行轨迹')}</span>
        <span className='usage-log-routing-trace-note'>
          {t('按处理阶段整理，不代表精确时间顺序')}
        </span>
      </div>
      {traceGroups.length === 0 ? (
        <div className='usage-log-routing-trace-empty'>
          {t('没有记录到路由动作')}
        </div>
      ) : (
        <div className='usage-log-routing-trace-groups'>
          {traceGroups.map((group) => (
            <section
              className='usage-log-routing-trace-stage'
              key={group.stage}
            >
              <h4 className='usage-log-routing-trace-stage-title'>
                {getResponsesRoutingTraceStageDisplay(group.stage, t)}
              </h4>
              <div className='usage-log-routing-trace-events'>
                {group.items.map((item, index) => {
                  const display = getResponsesRoutingEventDisplay(
                    item.event,
                    t,
                  );
                  const tone = getResponsesRoutingTraceTone(item.event);
                  return (
                    <div
                      className={`usage-log-routing-trace-event is-${tone}`}
                      key={`${group.stage}:${display.label}:${index}`}
                    >
                      <span className='usage-log-routing-trace-event-label'>
                        {display.label}
                      </span>
                      <span
                        className={[
                          'usage-log-routing-trace-event-value',
                          codeLikeEvents.has(item.event.code) ? 'is-code' : '',
                        ]
                          .filter(Boolean)
                          .join(' ')}
                      >
                        {display.value}
                      </span>
                    </div>
                  );
                })}
              </div>
            </section>
          ))}
        </div>
      )}
      {showChannelTrace && channelTrace ? (
        <section className='usage-log-routing-trace-channel'>
          <h4 className='usage-log-routing-trace-stage-title'>
            {t('路由选择')}
          </h4>
          <div className='usage-log-routing-trace-events'>
            {channelTrace.candidates.length > 0 && (
              <div className='usage-log-routing-trace-event is-neutral'>
                <span className='usage-log-routing-trace-event-label'>
                  {t('渠道 ID')}
                </span>
                <span className='usage-log-routing-trace-event-value is-code'>
                  {channelTrace.candidates
                    .map((channelId) => formatChannelLabel(channelId, t))
                    .join(' -> ')}
                </span>
              </div>
            )}
            {channelTrace.preflightSkipped.map((skip) => (
              <div
                className='usage-log-routing-trace-event is-recovery'
                key={`skip:${skip.channel_id}:${skip.reason}`}
              >
                <span className='usage-log-routing-trace-event-label'>
                  {t('跳过渠道')}
                </span>
                <span className='usage-log-routing-trace-event-value is-code'>
                  {t('{{channel}}；{{reason}}', {
                    channel: formatChannelLabel(skip.channel_id, t),
                    reason: skip.reason,
                  })}
                </span>
              </div>
            ))}
            {channelTrace.attemptedChannels.map((attempt, index) => (
              <div
                className='usage-log-routing-trace-event is-neutral'
                key={`attempt:${attempt.channel_id}:${index}`}
              >
                <span className='usage-log-routing-trace-event-label'>
                  {t('上游尝试')}
                </span>
                <span className='usage-log-routing-trace-event-value is-code'>
                  {formatChannelAttempt(attempt, t)}
                </span>
              </div>
            ))}
            {channelTrace.finalChannel !== undefined && (
              <div className='usage-log-routing-trace-event is-neutral'>
                <span className='usage-log-routing-trace-event-label'>
                  {t('渠道')}
                </span>
                <span className='usage-log-routing-trace-event-value is-code'>
                  {formatChannelLabel(channelTrace.finalChannel, t)}
                </span>
              </div>
            )}
            {channelTrace.terminalReason && (
              <div className='usage-log-routing-trace-event is-neutral'>
                <span className='usage-log-routing-trace-event-label'>
                  {t('失败原因')}
                </span>
                <span className='usage-log-routing-trace-event-value is-code'>
                  {channelTrace.terminalReason}
                </span>
              </div>
            )}
          </div>
        </section>
      ) : null}
    </div>
  );
}

function getCapabilityState(value) {
  if (value === true) {
    return 'supported';
  }
  if (value === false) {
    return 'unsupported';
  }
  return 'unknown';
}

function getReasoningContextCapabilityState(capability) {
  if (capability.reasoning_context_capability === 'supported') {
    return 'supported';
  }
  if (capability.reasoning_context_capability === 'unsupported') {
    return 'unsupported';
  }
  if (capability.reasoning_context_capability === 'unknown') {
    return 'unknown';
  }
  return getCapabilityState(capability.supports_reasoning_context);
}

function formatCapabilityState(state, t) {
  switch (state) {
    case 'supported':
      return t('支持');
    case 'unsupported':
      return t('不支持');
    default:
      return t('未知');
  }
}

function formatBooleanState(value, t) {
  if (value === true) {
    return t('是');
  }
  if (value === false) {
    return t('否');
  }
  return t('未知');
}

function CapabilityEvidence({ other, t }) {
  const capability = other?.channel_capability_snapshot;
  const observedReason =
    other?.admin_info?.responses_channel_capability_observed?.reason;
  const probeReason =
    other?.admin_info?.responses_channel_capability_probe?.reason;

  if (!capability) {
    return (
      <section className='usage-log-routing-capabilities'>
        <div className='usage-log-routing-capabilities-header'>
          <span>{t('能力依据')}</span>
          <span className='usage-log-routing-capabilities-unrecorded'>
            {t('未记录能力快照')}
          </span>
        </div>
      </section>
    );
  }

  const capabilityRows = [
    [t('Responses 能力'), getCapabilityState(capability.supports_responses)],
    [
      t('Responses Compact 能力'),
      getCapabilityState(capability.supports_responses_compact),
    ],
    [
      t('REST Previous ID'),
      getCapabilityState(capability.supports_rest_previous_response_id),
    ],
    [
      t('压缩项透传'),
      getCapabilityState(capability.supports_compaction_item_passthrough),
    ],
    [t('自定义工具'), getCapabilityState(capability.supports_custom_tools)],
    [
      t('命名空间工具'),
      getCapabilityState(capability.supports_namespace_tools),
    ],
    [t('工具搜索'), getCapabilityState(capability.supports_tool_search)],
    [
      t('流用量能力'),
      getCapabilityState(capability.supports_stream_options_include_usage),
    ],
    [t('推理上下文能力'), getReasoningContextCapabilityState(capability)],
  ];

  return (
    <section className='usage-log-routing-capabilities'>
      <div className='usage-log-routing-capabilities-header'>
        <span>{t('能力依据')}</span>
      </div>
      {(capability.source ||
        capability.profile ||
        capability.compact_mode_effective) && (
        <div className='usage-log-routing-capability-meta'>
          {capability.source && (
            <div>
              <span>{t('能力来源')}</span>
              <code>{capability.source}</code>
            </div>
          )}
          {capability.profile && (
            <div>
              <span>{t('上游配置')}</span>
              <code>{capability.profile}</code>
            </div>
          )}
          {capability.compact_mode_effective && (
            <div>
              <span>{t('生效 Compact 模式')}</span>
              <code>{capability.compact_mode_effective}</code>
            </div>
          )}
        </div>
      )}
      <div className='usage-log-routing-capability-grid'>
        {capabilityRows.map(([label, state]) => (
          <div className='usage-log-routing-capability-item' key={label}>
            <span>{label}</span>
            <strong className={`is-${state}`}>
              {formatCapabilityState(state, t)}
            </strong>
          </div>
        ))}
        <div className='usage-log-routing-capability-item'>
          <span>{t('会剥离推理上下文')}</span>
          <strong>
            {formatBooleanState(
              capability.strips_responses_reasoning_context,
              t,
            )}
          </strong>
        </div>
        <div className='usage-log-routing-capability-item'>
          <span>{t('会剥离加密推理')}</span>
          <strong>
            {formatBooleanState(
              capability.strips_responses_encrypted_reasoning,
              t,
            )}
          </strong>
        </div>
      </div>
      {(observedReason || probeReason) && (
        <div className='usage-log-routing-capability-notes'>
          {observedReason && (
            <div>
              <span>{t('能力观测原因')}</span>
              <code>{observedReason}</code>
            </div>
          )}
          {probeReason && (
            <div>
              <span>{t('能力探测结果')}</span>
              <code>{probeReason}</code>
            </div>
          )}
        </div>
      )}
    </section>
  );
}

export function ResponsesRoutingTooltip({ other, t }) {
  const result = getResponsesRoutingResult(other);
  if (!result) {
    return null;
  }

  return (
    <div className='usage-log-routing-tooltip'>
      <div className='usage-log-routing-tooltip-header'>
        <span className='usage-log-header-audit-label-tag usage-log-routing-tooltip-label-tag'>
          {t('Responses 路由')}
        </span>
      </div>
      <RoutingOverview other={other} t={t} />
      <ResponsesRoutingTrace other={other} t={t} compact />
    </div>
  );
}

export function ResponsesRoutingResult({ other, t }) {
  const result = getResponsesRoutingResult(other);
  if (!result) {
    return null;
  }

  const resultDisplay = getResponsesRoutingResultDisplay(result, t);
  return (
    <Tooltip
      className='usage-log-info-popover-layer'
      content={<ResponsesRoutingTooltip other={other} t={t} />}
      position='top'
      showArrow
    >
      <span>
        <Tag color={resultDisplay.color} shape='circle'>
          <Space spacing='tight'>
            <span>{resultDisplay.label}</span>
            <span
              className='usage-log-routing-action-count'
              title={t('已记录动作：{{count}}', {
                count: result.actionCount,
              })}
            >
              {result.actionCount}
            </span>
          </Space>
        </Tag>
      </span>
    </Tooltip>
  );
}

export function ResponsesRoutingDetails({ other, t }) {
  const result = getResponsesRoutingResult(other);
  if (!result) {
    return null;
  }

  return (
    <div className='usage-log-routing-details'>
      <RoutingOverview other={other} t={t} />
      <ResponsesRoutingTrace other={other} t={t} />
      <CapabilityEvidence other={other} t={t} />
    </div>
  );
}

function formatUserRoutingOutcome(outcome, t) {
  switch (outcome) {
    case 'success':
      return t('成功');
    case 'rate_limited':
      return t('错误');
    case 'authentication_failed':
      return t('错误');
    case 'channel_unavailable':
    case 'error':
      return t('错误');
    default:
      return t('未知');
  }
}

export function ResponsesRoutingUserSummaryDetails({ summary, t }) {
  if (!summary || typeof summary !== 'object') {
    return null;
  }

  const reasons = Array.isArray(summary.reasons)
    ? summary.reasons.filter((reason) => String(reason || '').trim())
    : [];
  return (
    <section className='usage-log-routing-capabilities usage-log-routing-user-summary'>
      <div className='usage-log-routing-capabilities-header'>
        <span>{t('Responses 路由')}</span>
      </div>
      <div className='usage-log-routing-capability-grid'>
        <div className='usage-log-routing-capability-item'>
          <span>{t('最终结果')}</span>
          <strong>{formatUserRoutingOutcome(summary.outcome, t)}</strong>
        </div>
        <div className='usage-log-routing-capability-item'>
          <span>{t('路由决策')}</span>
          <strong>{summary.automatic_reroute ? t('是') : t('否')}</strong>
        </div>
        <div className='usage-log-routing-capability-item'>
          <span>{t('已记录动作')}</span>
          <strong>{Number(summary.attempt_count || 0)}</strong>
        </div>
        <div className='usage-log-routing-capability-item'>
          <span>{t('跳过渠道')}</span>
          <strong>{Number(summary.preflight_skip_count || 0)}</strong>
        </div>
        {reasons.length > 0 && (
          <div className='usage-log-routing-capability-item'>
            <span>{t('失败原因')}</span>
            <code>{reasons.join(', ')}</code>
          </div>
        )}
      </div>
    </section>
  );
}
