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

import { describe, expect, test } from 'bun:test';

import { resolveLobeHubIconTarget } from '../src/helpers/lobeHubIconResolver.js';

describe('classic LobeHub icon resolver', () => {
  test('ignores the first unresolved variant segment', () => {
    const BaseIcon = () => null;

    const result = resolveLobeHubIconTarget(
      BaseIcon,
      'OpenRouter.Color.size=20',
    );

    expect(result.iconComponent).toBe(BaseIcon);
    expect(result.propSegments).toEqual(['size=20']);
  });

  test('keeps resolved child component segment as icon target', () => {
    const BaseIcon = () => null;
    const AvatarIcon = () => null;
    BaseIcon.Avatar = AvatarIcon;

    const result = resolveLobeHubIconTarget(
      BaseIcon,
      "OpenRouter.Avatar.shape='square'",
    );

    expect(result.iconComponent).toBe(AvatarIcon);
    expect(result.propSegments).toEqual(["shape='square'"]);
  });
});
