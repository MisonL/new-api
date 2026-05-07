import { useTranslation } from 'react-i18next'
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'

type GroupPricingGuideProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

function GuideCodeBlock(props: { children: string }) {
  return (
    <pre className='bg-muted/60 overflow-x-auto rounded-lg border px-3 py-2 text-xs leading-6 whitespace-pre-wrap'>
      {props.children}
    </pre>
  )
}

export function GroupPricingGuide(props: GroupPricingGuideProps) {
  const { t } = useTranslation()

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent side='right' className='w-full gap-0 p-0 sm:max-w-2xl'>
        <SheetHeader className='border-b p-4'>
          <SheetTitle>{t('Group pricing usage guide')}</SheetTitle>
          <SheetDescription>
            {t(
              'Understand how user groups, token groups, ratios, and special rules work together.'
            )}
          </SheetDescription>
        </SheetHeader>

        <div className='space-y-5 overflow-y-auto p-4'>
          <section className='space-y-2'>
            <h3 className='text-sm font-semibold'>{t('Core concepts')}</h3>
            <div className='text-muted-foreground space-y-2 text-sm leading-6'>
              <p>
                <span className='text-foreground font-medium'>
                  {t('User group')}
                </span>
                {': '}
                {t(
                  'Assigned by administrators and used to represent a user level, such as default or vip.'
                )}
              </p>
              <p>
                <span className='text-foreground font-medium'>
                  {t('Token group')}
                </span>
                {': '}
                {t(
                  'Selected when creating a token and used as the default billing group for API calls.'
                )}
              </p>
              <p>
                <span className='text-foreground font-medium'>
                  {t('Ratio')}
                </span>
                {': '}
                {t(
                  'A billing multiplier. Lower ratios mean lower API call costs.'
                )}
              </p>
              <p>
                <span className='text-foreground font-medium'>
                  {t('User selectable')}
                </span>
                {': '}
                {t(
                  'When enabled, users can pick this group when creating tokens.'
                )}
              </p>
            </div>
          </section>

          <Accordion className='rounded-lg border px-3' type='single'>
            <AccordionItem value='groups'>
              <AccordionTrigger>{t('Pricing group example')}</AccordionTrigger>
              <AccordionContent className='space-y-3'>
                <p className='text-muted-foreground text-sm leading-6'>
                  {t(
                    'Use the pricing group table to manage the ratio and whether the group appears in the token creation dropdown.'
                  )}
                </p>
                <GuideCodeBlock>
                  {`${t('Group name')}   ${t('Ratio')}   ${t('User selectable')}   ${t('Description')}
standard     1.0     ${t('Yes')}               ${t('Standard price')}
premium      0.5     ${t('Yes')}               ${t('Premium plan, half price')}
vip          0.5     ${t('No')}                ${t('Assigned by administrator only')}`}
                </GuideCodeBlock>
                <p className='text-muted-foreground text-sm leading-6'>
                  {t(
                    'Users only see groups marked as user selectable. Non-selectable groups can still be assigned by administrators.'
                  )}
                </p>
              </AccordionContent>
            </AccordionItem>

            <AccordionItem value='auto'>
              <AccordionTrigger>{t('Auto group behavior')}</AccordionTrigger>
              <AccordionContent className='space-y-3'>
                <p className='text-muted-foreground text-sm leading-6'>
                  {t(
                    'When a token uses the auto group, the system tries groups from top to bottom until it finds an available group.'
                  )}
                </p>
                <GuideCodeBlock>{`["default", "vip"]`}</GuideCodeBlock>
                <p className='text-muted-foreground text-sm leading-6'>
                  {t(
                    'If default auto group is enabled, newly created tokens start with auto instead of an empty group.'
                  )}
                </p>
              </AccordionContent>
            </AccordionItem>

            <AccordionItem value='special-ratio'>
              <AccordionTrigger>{t('Special ratio rules')}</AccordionTrigger>
              <AccordionContent className='space-y-3'>
                <p className='text-muted-foreground text-sm leading-6'>
                  {t(
                    'Special ratios override the token group ratio for specific user group and token group combinations.'
                  )}
                </p>
                <GuideCodeBlock>{`{
  "vip": {
    "standard": 0.8,
    "premium": 0.3
  }
}`}</GuideCodeBlock>
                <p className='text-muted-foreground text-sm leading-6'>
                  {t(
                    'Only configured combinations are overridden. All other calls keep the token group base ratio.'
                  )}
                </p>
              </AccordionContent>
            </AccordionItem>

            <AccordionItem value='usable'>
              <AccordionTrigger>
                {t('Special usable group rules')}
              </AccordionTrigger>
              <AccordionContent className='space-y-3'>
                <p className='text-muted-foreground text-sm leading-6'>
                  {t(
                    'Special usable group rules can add, remove, or append selectable token groups for a specific user group.'
                  )}
                </p>
                <GuideCodeBlock>{`{
  "vip": {
    "+:premium": "${t('Premium plan, half price')}",
    "-:default": "remove",
    "special": "${t('Special group')}"
  }
}`}</GuideCodeBlock>
                <p className='text-muted-foreground text-sm leading-6'>
                  {t(
                    'Use +: to add a group, -: to remove a default selectable group, or no prefix to append a group.'
                  )}
                </p>
              </AccordionContent>
            </AccordionItem>
          </Accordion>
        </div>
      </SheetContent>
    </Sheet>
  )
}
