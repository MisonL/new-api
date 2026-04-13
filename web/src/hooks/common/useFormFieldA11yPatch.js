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

import { useEffect } from 'react';

const PATCH_INTERVAL_MS = 800;

const useFormFieldA11yPatch = (routeKey) => {
  useEffect(() => {
    let generatedFieldCounter = 0;
    let isPatching = false;
    let patchScheduled = false;

    const toStableToken = (value, fallback) => {
      const token = String(value || '')
        .trim()
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, '-')
        .replace(/^-+|-+$/g, '');
      return token || fallback;
    };

    const buildGeneratedFieldId = (element) => {
      const source =
        element.getAttribute('data-insp-path') ||
        element.getAttribute('aria-label') ||
        element.getAttribute('placeholder') ||
        element.closest('label')?.textContent ||
        element.closest('.semi-form-field')?.textContent ||
        element.id ||
        element.name ||
        element.type ||
        element.tagName.toLowerCase();
      generatedFieldCounter += 1;
      return `audit-field-${toStableToken(source, 'field')}-${generatedFieldCounter}`;
    };

    const ensureFieldId = (element, fallbackId) => {
      if (!element) {
        return null;
      }
      const nextId = fallbackId || buildGeneratedFieldId(element);
      if (!element.id) {
        element.id = nextId;
      }
      if (!element.name) {
        element.name = element.id;
      }
      return element.id;
    };

    const isNativeLabelableField = (element) => {
      if (!element) {
        return false;
      }
      return ['input', 'textarea', 'select'].includes(
        element.tagName.toLowerCase(),
      );
    };

    const replaceLabelWithTextContainer = (label, labelId) => {
      if (!label || label.tagName.toLowerCase() !== 'label') {
        return label;
      }
      const container = document.createElement('div');
      Array.from(label.attributes).forEach((attr) => {
        if (attr.name !== 'for') {
          container.setAttribute(attr.name, attr.value);
        }
      });
      container.id = labelId || label.id;
      while (label.firstChild) {
        container.appendChild(label.firstChild);
      }
      label.replaceWith(container);
      return container;
    };

    const isVisibleField = (element) => {
      if (!element) {
        return false;
      }
      if (element.type === 'hidden' || element.hidden) {
        return false;
      }
      if (element.getAttribute('aria-hidden') === 'true') {
        return false;
      }
      if (element.closest('[aria-hidden="true"]')) {
        return false;
      }
      const style = window.getComputedStyle(element);
      return style.display !== 'none' && style.visibility !== 'hidden';
    };

    const getFieldLabelText = (element) => {
      const formField = element.closest('.semi-form-field');
      const explicitLabel = formField
        ?.querySelector('.semi-form-field-label-text')
        ?.textContent?.trim();
      if (explicitLabel) {
        return explicitLabel;
      }

      const placeholder = element.getAttribute('placeholder')?.trim();
      if (placeholder) {
        return placeholder;
      }

      const parentText = element.parentElement?.textContent?.trim();
      if (parentText) {
        return parentText.slice(0, 40);
      }

      return element.getAttribute('name') || element.id || 'form-field';
    };

    const patchDocumentFields = () => {
      if (typeof document === 'undefined' || !document.body) {
        return;
      }
      if (isPatching) {
        return;
      }

      isPatching = true;
      try {
        document
          .querySelectorAll('textarea[aria-hidden="true"]:not([name])')
          .forEach((element, index) => {
            element.setAttribute('name', `audit-hidden-textarea-${index + 1}`);
          });

        document
          .querySelectorAll('[data-a11y-disable-autocomplete]')
          .forEach((element) => {
            if (!isVisibleField(element)) {
              return;
            }
            element.setAttribute('autocomplete', 'off');
          });

        document
          .querySelectorAll(
            'input, textarea, select, [role="combobox"], [role="spinbutton"], [role="switch"]',
          )
          .forEach((element) => {
            const isHiddenInput =
              element.tagName.toLowerCase() === 'input' &&
              element.type === 'hidden';

            if (!isHiddenInput && !element.hidden) {
              if (!element.id || !element.getAttribute('name')) {
                ensureFieldId(element);
              }
            }

            if (!isVisibleField(element)) {
              return;
            }

            const currentAriaLabel = element.getAttribute('aria-label')?.trim();
            const hasGenericAriaLabel =
              currentAriaLabel === 'input value' ||
              currentAriaLabel === 'selected';
            const hasAssociatedLabel =
              (!!element.id &&
                document.querySelector(`label[for="${element.id}"]`)) ||
              !!element.getAttribute('aria-label') ||
              !!element.getAttribute('aria-labelledby');

            if (!hasAssociatedLabel || hasGenericAriaLabel) {
              element.setAttribute('aria-label', getFieldLabelText(element));
            }
          });

        document.querySelectorAll('label[for]').forEach((label, index) => {
          const targetId = label.getAttribute('for');
          const field = label.closest('.semi-form-field');

          if (!targetId) {
            const control = field?.querySelector('input, textarea, select');
            const controlId = ensureFieldId(
              control,
              `audit-form-control-${index + 1}`,
            );
            if (controlId) {
              label.htmlFor = controlId;
            } else {
              label.removeAttribute('for');
            }
            return;
          }

          const target = document.getElementById(targetId);
          if (!target) {
            const fallbackControl = field?.querySelector(
              'input, textarea, select, [role="combobox"], [role="spinbutton"]',
            );
            const fallbackId = ensureFieldId(
              fallbackControl,
              `${targetId}-control-${index + 1}`,
            );
            if (fallbackId) {
              if (
                fallbackControl &&
                ['input', 'textarea', 'select'].includes(
                  fallbackControl.tagName.toLowerCase(),
                )
              ) {
                label.htmlFor = fallbackId;
              } else {
                label.removeAttribute('for');
                const labelId = label.id || `audit-form-label-${index + 1}`;
                label.id = labelId;
                fallbackControl?.setAttribute('aria-labelledby', labelId);
              }
            } else {
              label.removeAttribute('for');
            }
            return;
          }

          if (isNativeLabelableField(target)) {
            return;
          }

          const control = target.querySelector('input, textarea, select');
          const controlId = ensureFieldId(control, `${targetId}-control`);
          if (controlId) {
            if (target.id === targetId) {
              target.id = `${targetId}-wrapper`;
            }
            label.htmlFor = controlId;
          } else {
            label.removeAttribute('for');
          }
        });

        document
          .querySelectorAll('.semi-form-field > label:not([for])')
          .forEach((label, index) => {
            if (label.querySelector('input, textarea, select')) {
              return;
            }

            const field = label.closest('.semi-form-field');
            const control =
              field?.querySelector(
                'input, textarea, select, [role="combobox"], [role="spinbutton"]',
              ) || null;
            const labelId = label.id || `audit-form-label-${index + 1}`;
            const labelNode = isNativeLabelableField(control)
              ? label
              : replaceLabelWithTextContainer(label, labelId);

            labelNode.id = labelId;

            if (control && isNativeLabelableField(control)) {
              const controlId = ensureFieldId(
                control,
                `audit-form-control-${index + 1}`,
              );
              if (controlId) {
                labelNode.htmlFor = controlId;
              }
              return;
            }

            if (control && !control.getAttribute('aria-labelledby')) {
              control.setAttribute('aria-labelledby', labelId);
            }
          });

        document
          .querySelectorAll(
            'input[aria-labelledby], textarea[aria-labelledby], select[aria-labelledby], [role="combobox"][aria-labelledby], [role="spinbutton"][aria-labelledby], [role="switch"][aria-labelledby]',
          )
          .forEach((control, index) => {
            const ids = (control.getAttribute('aria-labelledby') || '')
              .split(/\s+/)
              .filter(Boolean);
            const field = control.closest('.semi-form-field');
            const labelText =
              field
                ?.querySelector('.semi-form-field-label-text')
                ?.textContent?.trim() ||
              control.getAttribute('placeholder') ||
              control.getAttribute('name') ||
              control.id ||
              `field-${index + 1}`;

            ids.forEach((id) => {
              if (document.getElementById(id)) {
                return;
              }
              const span = document.createElement('span');
              span.id = id;
              span.textContent = labelText;
              span.className = 'sr-only';
              span.style.position = 'absolute';
              span.style.width = '1px';
              span.style.height = '1px';
              span.style.padding = '0';
              span.style.margin = '-1px';
              span.style.overflow = 'hidden';
              span.style.clip = 'rect(0, 0, 0, 0)';
              span.style.whiteSpace = 'nowrap';
              span.style.border = '0';
              (field || document.body).prepend(span);
            });
          });

        document
          .querySelectorAll(
            '[role="combobox"][aria-activedescendant], [role="listbox"][aria-activedescendant]',
          )
          .forEach((control) => {
            const activeDescendantId = control.getAttribute('aria-activedescendant');
            if (!activeDescendantId) {
              return;
            }
            if (document.getElementById(activeDescendantId)) {
              return;
            }
            const fallbackActiveDescendant = document.createElement('span');
            fallbackActiveDescendant.id = activeDescendantId;
            fallbackActiveDescendant.textContent =
              control.textContent?.trim() ||
              control.getAttribute('aria-label') ||
              activeDescendantId;
            fallbackActiveDescendant.className = 'sr-only';
            fallbackActiveDescendant.style.position = 'absolute';
            fallbackActiveDescendant.style.width = '1px';
            fallbackActiveDescendant.style.height = '1px';
            fallbackActiveDescendant.style.padding = '0';
            fallbackActiveDescendant.style.margin = '-1px';
            fallbackActiveDescendant.style.overflow = 'hidden';
            fallbackActiveDescendant.style.clip = 'rect(0, 0, 0, 0)';
            fallbackActiveDescendant.style.whiteSpace = 'nowrap';
            fallbackActiveDescendant.style.border = '0';
            const field =
              control.closest('.semi-form-field-main') ||
              control.closest('.semi-form-field') ||
              control.parentElement ||
              document.body;
            field.prepend(fallbackActiveDescendant);
          });
      } finally {
        isPatching = false;
      }
    };

    const schedulePatch = () => {
      if (patchScheduled) {
        return;
      }
      patchScheduled = true;
      window.requestAnimationFrame(() => {
        patchScheduled = false;
        patchDocumentFields();
      });
    };

    patchDocumentFields();
    const delayedPatchTimers = [
      window.setTimeout(schedulePatch, 0),
      window.setTimeout(schedulePatch, 300),
      window.setTimeout(schedulePatch, 1000),
      window.setTimeout(schedulePatch, 2000),
      window.setTimeout(schedulePatch, 4000),
    ];
    const patchIntervalId = window.setInterval(schedulePatch, PATCH_INTERVAL_MS);

    const observer = new MutationObserver(() => {
      schedulePatch();
    });
    observer.observe(document.body, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: [
        'aria-hidden',
        'aria-label',
        'aria-labelledby',
        'aria-activedescendant',
        'type',
        'placeholder',
        'value',
        'checked',
        'class',
      ],
    });

    return () => {
      observer.disconnect();
      delayedPatchTimers.forEach((timerId) => window.clearTimeout(timerId));
      window.clearInterval(patchIntervalId);
    };
  }, [routeKey]);
};

export default useFormFieldA11yPatch;
