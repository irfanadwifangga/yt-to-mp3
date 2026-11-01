import {
  Children,
  isValidElement,
  useEffect,
  useId,
  useRef,
  useState,
  type SelectHTMLAttributes,
} from "react";
import { createPortal } from "react-dom";

/** Keeps the form control and its label, with a themed popup instead of the OS menu. */
export function Select({ children, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  const control = useRef<HTMLSelectElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const id = useId();
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const [position, setPosition] = useState({ left: 0, top: 0, width: 0, maxHeight: 240 });
  const options = Children.toArray(children).filter(
    isValidElement<{ value?: string | number; disabled?: boolean; children?: React.ReactNode }>,
  );
  const selected = Math.max(
    0,
    options.findIndex((o) => String(o.props.value ?? o.props.children) === String(props.value)),
  );
  function show() {
    if (props.disabled || !control.current) return;
    const r = control.current.getBoundingClientRect();
    const height = Math.min(240, options.length * 34 + 8);
    const above = innerHeight - r.bottom < height && r.top > innerHeight - r.bottom;
    setPosition({
      left: Math.max(8, Math.min(r.left, innerWidth - Math.max(r.width, 130) - 8)),
      top: above ? Math.max(8, r.top - height - 4) : r.bottom + 4,
      width: Math.max(r.width, 130),
      maxHeight: above ? r.top - 12 : innerHeight - r.bottom - 12,
    });
    setActive(selected);
    setOpen(true);
  }
  function choose(index: number) {
    const option = options[index];
    if (!option || option.props.disabled || !control.current) return;
    const value = String(option.props.value ?? option.props.children);
    const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, "value")!.set!;
    setter.call(control.current, value);
    control.current.dispatchEvent(new Event("change", { bubbles: true }));
    setOpen(false);
    control.current.focus();
  }
  useEffect(() => {
    if (!open) return;
    function outside(event: PointerEvent) {
      if (
        !control.current?.contains(event.target as Node) &&
        !menu.current?.contains(event.target as Node)
      )
        setOpen(false);
    }
    function moved(event: Event) {
      if (!(event.target instanceof Node) || !menu.current?.contains(event.target)) setOpen(false);
    }
    document.addEventListener("pointerdown", outside);
    window.addEventListener("resize", moved);
    document.addEventListener("scroll", moved, true);
    return () => {
      document.removeEventListener("pointerdown", outside);
      window.removeEventListener("resize", moved);
      document.removeEventListener("scroll", moved, true);
    };
  }, [open]);
  useEffect(() => {
    const list = menu.current;
    const option = list?.querySelector<HTMLElement>(`[data-index="${active}"]`);
    if (!list || !option) return;
    if (option.offsetTop < list.scrollTop) list.scrollTop = option.offsetTop;
    else if (option.offsetTop + option.offsetHeight > list.scrollTop + list.clientHeight)
      list.scrollTop = option.offsetTop + option.offsetHeight - list.clientHeight;
  }, [active, open]);
  return (
    <>
      <select
        {...props}
        ref={control}
        className={`themed-select ${props.className ?? ""}`}
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        aria-haspopup="listbox"
        aria-activedescendant={open ? `${id}-${active}` : undefined}
        onMouseDown={(e) => {
          e.preventDefault();
          control.current?.focus();
        }}
        onClick={() => (open ? setOpen(false) : show())}
        onBlur={() => setOpen(false)}
        onChange={(e) => {
          setOpen(false);
          props.onChange?.(e);
        }}
        onKeyDown={(e) => {
          if (e.key === "Tab") {
            setOpen(false);
            return;
          }
          if (e.key === "Escape" && open) {
            e.preventDefault();
            e.stopPropagation();
            setOpen(false);
            return;
          }
          if (["ArrowDown", "ArrowUp", "Home", "End", "Enter", " "].includes(e.key)) {
            e.preventDefault();
            if (!open) {
              show();
              return;
            }
            if (e.key === "Enter" || e.key === " ") choose(active);
            else {
              const direction = e.key === "ArrowUp" ? -1 : 1;
              let next =
                e.key === "Home"
                  ? 0
                  : e.key === "End"
                    ? options.length - 1
                    : Math.max(0, Math.min(options.length - 1, active + direction));
              while (options[next]?.props.disabled && next >= 0 && next < options.length)
                next += direction;
              if (next >= 0 && next < options.length) setActive(next);
            }
          } else if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
            e.preventDefault();
            if (!open) show();
            const next = options.findIndex(
              (o) =>
                !o.props.disabled &&
                String(o.props.children).toLocaleLowerCase().startsWith(e.key.toLocaleLowerCase()),
            );
            if (next >= 0) setActive(next);
          }
        }}>
        {children}
      </select>
      {open &&
        createPortal(
          <div
            id={id}
            ref={menu}
            className="select-popup"
            role="listbox"
            aria-label={props["aria-label"] ?? control.current?.labels?.[0]?.textContent ?? ""}
            style={position}
            onMouseDown={(e) => e.preventDefault()}>
            {options.map((o, i) => (
              <div
                key={String(o.key ?? i)}
                id={`${id}-${i}`}
                role="option"
                aria-selected={i === selected}
                aria-disabled={o.props.disabled || undefined}
                data-index={i}
                data-active={i === active}
                onPointerMove={() => setActive(i)}
                onClick={() => choose(i)}>
                <span>{o.props.children}</span>
                <span aria-hidden="true">{i === selected ? "✓" : ""}</span>
              </div>
            ))}
          </div>,
          control.current?.closest("dialog") ?? document.body,
        )}
    </>
  );
}
