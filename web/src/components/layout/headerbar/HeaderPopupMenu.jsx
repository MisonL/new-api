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

import React, { useEffect, useRef, useState } from 'react';

const HeaderPopupMenu = ({
  menuLabel,
  menuClassName = '',
  renderTrigger,
  renderContent,
}) => {
  const [open, setOpen] = useState(false);
  const containerRef = useRef(null);

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
        <div
          role='menu'
          aria-label={menuLabel}
          className={`absolute right-0 top-full z-[120] mt-2 min-w-[144px] overflow-hidden rounded-lg border border-semi-color-border bg-semi-color-bg-overlay p-1 shadow-lg dark:border-gray-600 dark:bg-gray-700 ${menuClassName}`.trim()}
        >
          {renderContent({ closeMenu: () => setOpen(false) })}
        </div>
      )}
    </div>
  );
};

export default HeaderPopupMenu;
