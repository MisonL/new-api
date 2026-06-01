import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  getProtocolRuleWarningKeys,
  isResponsesToChatRule,
  type ProtocolRule,
} from './protocol-conversion-policy-utils'

type ProtocolConversionRuleImpactAlertProps = {
  rule: ProtocolRule
  passThroughEnabled: boolean
}

export function ProtocolConversionRuleImpactAlert({
  rule,
  passThroughEnabled,
}: ProtocolConversionRuleImpactAlertProps) {
  const { t } = useTranslation()
  const responsesToChat = isResponsesToChatRule(rule)
  const title = responsesToChat
    ? 'Responses to Chat Completions'
    : 'Chat Completions to Responses'
  const description = responsesToChat
    ? 'Requests will be adapted to Chat Completions before upstream forwarding. Custom tool bridge can rewrite Responses custom tools.'
    : 'Requests will be adapted to Responses before upstream forwarding. Request passthrough must stay disabled for this conversion to run.'

  return (
    <Alert>
      <AlertTitle>{t(title)}</AlertTitle>
      <AlertDescription className='space-y-2'>
        <div>{t(description)}</div>
        {[
          ...getProtocolRuleWarningKeys(rule),
          ...(passThroughEnabled
            ? [
                'Global request passthrough is enabled. Conversion will be skipped at runtime.',
              ]
            : []),
        ].map((warning) => (
          <div key={warning}>{t(warning)}</div>
        ))}
      </AlertDescription>
    </Alert>
  )
}
