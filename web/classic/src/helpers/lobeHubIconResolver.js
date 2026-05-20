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

const unsafeIconSegmentKeys = new Set([
  '__proto__',
  'constructor',
  'prototype',
]);

function hasSafeOwnIconSegment(baseIcon, segment) {
  return (
    segment &&
    !unsafeIconSegmentKeys.has(segment) &&
    Object.prototype.hasOwnProperty.call(baseIcon, segment)
  );
}

export function resolveLobeHubIconTarget(baseIcon, iconName) {
  const segments = String(iconName).split('.');
  let iconComponent = baseIcon;
  let propStartIndex = 1;

  if (
    baseIcon &&
    segments.length > 1 &&
    hasSafeOwnIconSegment(baseIcon, segments[1])
  ) {
    iconComponent = baseIcon[segments[1]];
    propStartIndex = 2;
  } else if (segments.length > 1 && !segments[1].includes('=')) {
    propStartIndex = 2;
  }

  return {
    iconComponent,
    propSegments: segments.slice(propStartIndex),
  };
}
