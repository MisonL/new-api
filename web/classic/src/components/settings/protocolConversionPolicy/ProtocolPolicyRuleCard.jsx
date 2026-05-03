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
import { Card } from '@douyinfe/semi-ui';
import { isRuleDirectionValid } from './utils';
import ProtocolPolicyRuleBody from './ProtocolPolicyRuleBody';
import ProtocolPolicyRuleSummary from './ProtocolPolicyRuleSummary';

export default function ProtocolPolicyRuleCard(props) {
  const {
    channelTypeOptions,
    index,
    isExpanded,
    removeRule,
    rule,
    ruleKey,
    t,
    toggleRuleExpanded,
    updateRule,
  } = props;
  const directionInvalid = !isRuleDirectionValid(rule);

  return (
    <Card
      bodyStyle={{ padding: 16 }}
      style={{
        borderRadius: 12,
        border: '1px solid var(--semi-color-border)',
        boxShadow: '0 1px 2px rgba(var(--semi-grey-9), 0.04)',
      }}
    >
      <ProtocolPolicyRuleSummary
        directionInvalid={directionInvalid}
        index={index}
        isExpanded={isExpanded}
        rule={rule}
        ruleKey={ruleKey}
        t={t}
        toggleRuleExpanded={toggleRuleExpanded}
        updateRule={updateRule}
      />
      {isExpanded ? (
        <ProtocolPolicyRuleBody
          channelTypeOptions={channelTypeOptions}
          directionInvalid={directionInvalid}
          index={index}
          removeRule={removeRule}
          rule={rule}
          t={t}
          updateRule={updateRule}
        />
      ) : null}
    </Card>
  );
}
