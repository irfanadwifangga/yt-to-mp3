import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { NumberInput } from "./NumberInput";
import { t } from "./i18n";

it("steps precisely and clamps the value to its limit", () => {
  const change = vi.fn();
  render(
    <NumberInput
      aria-label="Start"
      defaultValue="24.735"
      step="0.001"
      max="24.736"
      onChange={change}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: t("controls.increase") }));
  expect((screen.getByRole("spinbutton") as HTMLInputElement).value).toBe("24.736");
  expect(change).toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: t("controls.increase") }));
  expect((screen.getByRole("spinbutton") as HTMLInputElement).value).toBe("24.736");
});
