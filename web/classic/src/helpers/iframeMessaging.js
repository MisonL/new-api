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

export function getIframeTargetOrigin(iframe) {
  if (!iframe || typeof window === 'undefined') {
    return null;
  }

  const src = iframe.getAttribute('src') || iframe.src;
  if (!src) {
    return null;
  }

  try {
    const url = new URL(src, window.location.href);
    if (url.protocol !== 'http:' && url.protocol !== 'https:') {
      return null;
    }
    return url.origin;
  } catch (e) {
    return null;
  }
}

export function postMessageToIframe(iframe, message) {
  const targetOrigin = getIframeTargetOrigin(iframe);
  const contentWindow = iframe && iframe.contentWindow;
  if (!contentWindow || !targetOrigin) {
    return false;
  }
  contentWindow.postMessage(message, targetOrigin);
  return true;
}
