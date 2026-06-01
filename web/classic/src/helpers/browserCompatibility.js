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

function cloneSerializableValue(value, seen = new WeakMap()) {
  if (value === null || typeof value !== 'object') return value;

  if (seen.has(value)) return seen.get(value);

  if (value instanceof Date) return new Date(value.getTime());
  if (value instanceof RegExp) return new RegExp(value);
  if (value instanceof Map) {
    const copy = new Map();
    seen.set(value, copy);
    value.forEach((mapValue, mapKey) => {
      copy.set(
        cloneSerializableValue(mapKey, seen),
        cloneSerializableValue(mapValue, seen),
      );
    });
    return copy;
  }
  if (value instanceof Set) {
    const copy = new Set();
    seen.set(value, copy);
    value.forEach((setValue) => {
      copy.add(cloneSerializableValue(setValue, seen));
    });
    return copy;
  }
  if (Array.isArray(value)) {
    const copy = [];
    seen.set(value, copy);
    value.forEach((item, index) => {
      copy[index] = cloneSerializableValue(item, seen);
    });
    return copy;
  }

  const copy = Object.create(Object.getPrototypeOf(value));
  seen.set(value, copy);
  Reflect.ownKeys(value).forEach((key) => {
    copy[key] = cloneSerializableValue(value[key], seen);
  });
  return copy;
}

function installStructuredCloneFallback() {
  if (typeof globalThis.structuredClone === 'function') return;
  globalThis.structuredClone = (value) => cloneSerializableValue(value);
}

function installMatchMediaFallback() {
  if (typeof window === 'undefined') return;

  if (typeof window.matchMedia !== 'function') {
    window.matchMedia = (query) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      addListener: () => undefined,
      removeListener: () => undefined,
      dispatchEvent: () => false,
    });
    return;
  }

  const nativeMatchMedia = window.matchMedia.bind(window);
  window.matchMedia = (query) => {
    const queryList = nativeMatchMedia(query);
    if (
      typeof queryList.addEventListener !== 'function' &&
      typeof queryList.addListener === 'function'
    ) {
      queryList.addEventListener = (type, listener) => {
        if (type === 'change') queryList.addListener(listener);
      };
      queryList.removeEventListener = (type, listener) => {
        if (type === 'change') queryList.removeListener(listener);
      };
    }
    return queryList;
  };
}

function installResizeObserverFallback() {
  if (
    typeof window === 'undefined' ||
    typeof window.ResizeObserver === 'function'
  ) {
    return;
  }

  class CompatResizeObserver {
    constructor(callback) {
      this.callback = callback;
      this.elements = new Set();
      this.frame = 0;
      this.schedule = this.schedule.bind(this);
      window.addEventListener('resize', this.schedule);
    }

    observe(target) {
      this.elements.add(target);
      this.schedule();
    }

    unobserve(target) {
      this.elements.delete(target);
    }

    disconnect() {
      this.elements.clear();
      window.removeEventListener('resize', this.schedule);
      if (this.frame) cancelAnimationFrame(this.frame);
    }

    schedule() {
      if (this.frame) cancelAnimationFrame(this.frame);
      this.frame = requestAnimationFrame(() => {
        this.frame = 0;
        const entries = Array.from(this.elements).map((target) => ({
          target,
          contentRect: target.getBoundingClientRect(),
        }));
        this.callback(entries, this);
      });
    }
  }

  window.ResizeObserver = CompatResizeObserver;
}

function installIntersectionObserverFallback() {
  if (
    typeof window === 'undefined' ||
    typeof window.IntersectionObserver === 'function'
  ) {
    return;
  }

  class CompatIntersectionObserver {
    constructor(callback) {
      this.callback = callback;
      this.elements = new Set();
      this.root = null;
      this.rootMargin = '0px';
      this.thresholds = [0];
    }

    observe(target) {
      this.elements.add(target);
      requestAnimationFrame(() => {
        if (!this.elements.has(target)) return;
        const rect = target.getBoundingClientRect();
        this.callback(
          [
            {
              target,
              isIntersecting: true,
              intersectionRatio: 1,
              time: performance.now(),
              boundingClientRect: rect,
              intersectionRect: rect,
              rootBounds: null,
            },
          ],
          this,
        );
      });
    }

    unobserve(target) {
      this.elements.delete(target);
    }

    disconnect() {
      this.elements.clear();
    }

    takeRecords() {
      return [];
    }
  }

  window.IntersectionObserver = CompatIntersectionObserver;
}

installStructuredCloneFallback();
installMatchMediaFallback();
installResizeObserverFallback();
installIntersectionObserverFallback();

if (typeof window !== 'undefined') {
  Object.defineProperty(window, '__NEW_API_BROWSER_COMPATIBILITY__', {
    configurable: true,
    value: {
      intersectionObserver: typeof window.IntersectionObserver === 'function',
      matchMedia: typeof window.matchMedia === 'function',
      resizeObserver: typeof window.ResizeObserver === 'function',
      structuredClone: typeof globalThis.structuredClone === 'function',
    },
  });
}
