import { useRef, type InputHTMLAttributes } from "react";
import { t } from "./i18n";

export function NumberInput(props: InputHTMLAttributes<HTMLInputElement>) {
  const input = useRef<HTMLInputElement>(null);
  function step(direction: number) {
    const node = input.current;
    if (!node) return;
    const current = Number(node.value || props.min || 0);
    const next = Math.min(
      Number(props.max ?? Infinity),
      Math.max(
        Number(props.min ?? -Infinity),
        current + direction * Number(props.step === "any" ? 1 : (props.step ?? 1)),
      ),
    );
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(
      node,
      String(Number(next.toFixed(3))),
    );
    node.dispatchEvent(new Event("input", { bubbles: true }));
    node.focus();
  }
  return (
    <span className="number-control">
      <input {...props} type="number" ref={input} />
      <span className="number-steps">
        <button
          type="button"
          disabled={props.disabled || props.readOnly}
          aria-label={t("controls.increase")}
          onClick={() => step(1)}>
          +
        </button>
        <button
          type="button"
          disabled={props.disabled || props.readOnly}
          aria-label={t("controls.decrease")}
          onClick={() => step(-1)}>
          −
        </button>
      </span>
    </span>
  );
}
