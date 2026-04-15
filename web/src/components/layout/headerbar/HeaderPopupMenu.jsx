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

import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useActualTheme } from '../../../context/Theme';

const HeaderPopupMenu = ({
  menuLabel,
  menuClassName = '',
  renderTrigger,
  renderContent,
}) => {
  const [open, setOpen] = useState(false);
  const containerRef = useRef(null);
  const actualTheme = useActualTheme();

  const popupStyle = useMemo(
    () => ({
      backgroundColor:
        actualTheme === 'dark'
          ? 'rgba(39, 39, 42, 0.98)'
          : 'rgba(255, 255, 255, 0.98)',
      borderColor:
        actualTheme === 'dark'
          ? 'rgba(82, 82, 91, 0.92)'
          : 'rgba(203, 213, 225, 0.96)',
      boxShadow:
        actualTheme === 'dark'
          ? '0 20px 25px -5px rgba(0, 0, 0, 0.45), 0 8px 10px -6px rgba(0, 0, 0, 0.35)'
          : '0 20px 25px -5px rgba(15, 23, 42, 0.12), 0 8px 10px -6px rgba(15, 23, 42, 0.08)',
      isolation: 'isolate',
    }),
    [actualTheme],
  );

  useEffect(() => {
    const handlePointerDown = (event) => {
      if (!containerRef.current?.contains(event.target)) {
        setOpen(false);
      }
    };

    const handleEscape = (event) => {
      if (event.key === 'Escape') {
        setOpen(false);
      }
    };

    document.addEventListener('mousedown', handlePointerDown);
    document.addEventListener('keydown', handleEscape);
    return () => {
      document.removeEventListener('mousedown', handlePointerDown);
      document.removeEventListener('keydown', handleEscape);
    };
  }, []);

  return (
    <div ref={containerRef} className='relative'>
      {renderTrigger({
        open,
        toggle: () => setOpen((prev) => !prev),
        closeMenu: () => setOpen(false),
      })}
      {open && (
        <div className='absolute right-0 top-full z-[260] mt-2'>
          <div
            role='menu'
            aria-label={menuLabel}
            style={popupStyle}
            className={`min-w-[144px] overflow-hidden rounded-lg border border-slate-200 bg-white p-1 shadow-xl ring-1 ring-black/5 dark:border-zinc-700 dark:bg-zinc-800 dark:ring-white/10 ${menuClassName}`.trim()}
          >
            {renderContent({ closeMenu: () => setOpen(false) })}
          </div>
        </div>
      )}
    </div>
  );
};

export default HeaderPopupMenu;
