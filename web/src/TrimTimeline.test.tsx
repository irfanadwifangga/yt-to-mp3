import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { expect, it } from "vitest";
import { TrimTimeline } from "./TrimTimeline";
import { t } from "./i18n";

it("shows both precise boundaries and updates them without allowing crossed handles", () => {
  function Editor() {
    const [start, setStart] = useState(1250),
      [end, setEnd] = useState(3500);
    return (
      <TrimTimeline
        durationMS={10000}
        startMS={start}
        endMS={end}
        onStart={setStart}
        onEnd={setEnd}
        disabled={false}
      />
    );
  }
  render(<Editor />);
  expect(screen.getByRole("group", { name: t("trim.selection") })).toBeTruthy();
  expect(screen.getByText("0:01.250")).toBeTruthy();
  expect(screen.getByText("0:03.500")).toBeTruthy();
  const start = screen.getByRole("slider", { name: t("trim.startSlider") });
  const end = screen.getByRole("slider", { name: t("trim.endSlider") });
  fireEvent.change(start, { target: { value: "2000" } });
  expect(start.getAttribute("aria-valuetext")).toBe("0:02");
  fireEvent.change(start, { target: { value: "9000" } });
  expect((start as HTMLInputElement).value).toBe("3499");
  fireEvent.change(end, { target: { value: "1000" } });
  expect((end as HTMLInputElement).value).toBe("3500");
});
