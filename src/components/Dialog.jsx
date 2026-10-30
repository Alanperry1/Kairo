import React, { useEffect, useId, useRef } from 'react';
import { X } from 'lucide-react';

export default function Dialog({ title, eyebrow, onClose, children, className = '' }) {
  const ref = useRef(null);
  const titleID = useId();
  useEffect(() => {
    const previous = document.activeElement;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    const focusable = () => [...ref.current.querySelectorAll('button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), summary, [tabindex="0"]')].filter((element) => {
      // Collapsed advanced sections must not trap focus in invisible inputs.
      for (let parent = element; parent && parent !== ref.current; parent = parent.parentElement) {
        if (parent.hidden || getComputedStyle(parent).display === 'none') return false;
        if (parent.tagName === 'DETAILS' && !parent.open) {
          const summary = parent.querySelector(':scope > summary');
          if (!summary?.contains(element)) return false;
        }
      }
      return true;
    });
    (ref.current.querySelector('[data-autofocus]') || focusable()[0] || ref.current).focus();
    const handleKey = (event) => {
      if (event.key === 'Escape') { event.stopPropagation(); onClose(); }
      if (event.key !== 'Tab') return;
      const elements = focusable();
      const first = elements[0]; const last = elements.at(-1);
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
    };
    const dialog = ref.current;
    dialog.addEventListener('keydown', handleKey);
    return () => { dialog.removeEventListener('keydown', handleKey); document.body.style.overflow = previousOverflow; previous?.focus(); };
  }, [onClose]);
  return <div className="modal-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
    <section ref={ref} tabIndex={-1} className={`detail-modal ${className}`} role="dialog" aria-modal="true" aria-labelledby={titleID}>
      <button className="close-modal icon-button" aria-label="Close dialog" onClick={onClose}><X size={19}/></button>
      {eyebrow && <span className="eyebrow">{eyebrow}</span>}
      <h2 id={titleID}>{title}</h2>
      {children}
    </section>
  </div>;
}
