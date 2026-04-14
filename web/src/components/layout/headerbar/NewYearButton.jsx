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
import { Button } from '@douyinfe/semi-ui';
import { Gift } from 'lucide-react';
import HeaderPopupMenu from './HeaderPopupMenu';

const CONFETTI_STYLE_ID = 'new-api-new-year-confetti-style';
const CONFETTI_COLORS = [
  '#ef4444',
  '#f97316',
  '#f59e0b',
  '#10b981',
  '#3b82f6',
  '#8b5cf6',
];

function ensureConfettiStyles() {
  if (typeof document === 'undefined') {
    return;
  }

  if (document.getElementById(CONFETTI_STYLE_ID)) {
    return;
  }

  const style = document.createElement('style');
  style.id = CONFETTI_STYLE_ID;
  style.textContent = `
    @keyframes newApiConfettiFall {
      from {
        opacity: 1;
        transform: translate3d(0, -10px, 0) rotate(0deg);
      }
      to {
        opacity: 0;
        transform: translate3d(var(--confetti-x), var(--confetti-y), 0) rotate(var(--confetti-rotation));
      }
    }

    .new-api-confetti-piece {
      position: fixed;
      top: -12px;
      width: 10px;
      height: 16px;
      border-radius: 2px;
      pointer-events: none;
      z-index: 1600;
      animation-name: newApiConfettiFall;
      animation-timing-function: cubic-bezier(0.2, 0.8, 0.2, 1);
      animation-fill-mode: forwards;
      will-change: transform, opacity;
    }
  `;
  document.head.appendChild(style);
}

function launchConfettiBurst() {
  if (typeof document === 'undefined' || typeof window === 'undefined') {
    return;
  }

  ensureConfettiStyles();

  const fragment = document.createDocumentFragment();
  const pieceCount = 28;
  const viewportWidth = window.innerWidth;
  const viewportHeight = window.innerHeight;

  for (let index = 0; index < pieceCount; index += 1) {
    const piece = document.createElement('span');
    const driftX = Math.round((Math.random() - 0.5) * viewportWidth * 0.7);
    const driftY = Math.round(
      viewportHeight * (0.35 + Math.random() * 0.35),
    );
    const rotation = `${540 + Math.round(Math.random() * 360)}deg`;

    piece.className = 'new-api-confetti-piece';
    piece.style.left = `${Math.round(Math.random() * viewportWidth)}px`;
    piece.style.backgroundColor =
      CONFETTI_COLORS[index % CONFETTI_COLORS.length];
    piece.style.opacity = String(0.9 - Math.random() * 0.2);
    piece.style.animationDuration = `${1.8 + Math.random() * 1.4}s`;
    piece.style.animationDelay = `${Math.random() * 0.12}s`;
    piece.style.setProperty('--confetti-x', `${driftX}px`);
    piece.style.setProperty('--confetti-y', `${driftY}px`);
    piece.style.setProperty('--confetti-rotation', rotation);
    fragment.appendChild(piece);

    window.setTimeout(() => {
      piece.remove();
    }, 3600);
  }

  document.body.appendChild(fragment);
}

const NewYearButton = ({ isNewYear }) => {
  if (!isNewYear) {
    return null;
  }

  const handleNewYearClick = () => {
    launchConfettiBurst();
  };

  return (
    <HeaderPopupMenu
      menuLabel='New Year'
      renderTrigger={({ open, toggle }) => (
        <Button
          theme='borderless'
          type='tertiary'
          icon={<Gift size={18} strokeWidth={2} />}
          aria-label='New Year'
          aria-haspopup='menu'
          aria-expanded={open}
          onClick={toggle}
          className='!p-1.5 !text-current focus:!bg-semi-color-fill-1 dark:focus:!bg-gray-700 rounded-full'
        />
      )}
      renderContent={({ closeMenu }) => (
        <button
          type='button'
          role='menuitem'
          onClick={() => {
            handleNewYearClick();
            closeMenu();
          }}
          className='flex w-full items-center rounded-md px-3 py-2 text-left text-sm !text-semi-color-text-0 hover:!bg-semi-color-fill-1 dark:!text-gray-200 dark:hover:!bg-gray-600'
        >
          Happy New Year
        </button>
      )}
    />
  );
};

export default NewYearButton;
