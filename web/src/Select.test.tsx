import { fireEvent, render, screen, within } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { Select } from "./Select";

it("opens a themed menu and changes the form value by keyboard", () => {
  const change = vi.fn();
  render(
    <Select aria-label="Language" value="id" onChange={(e) => change(e.target.value)}>
      <option value="id">Indonesia</option>
      <option value="en">English</option>
    </Select>,
  );
  const control = screen.getByRole("combobox", { name: "Language" });
  fireEvent.keyDown(control, { key: "ArrowDown" });
  expect(within(screen.getByRole("listbox")).getAllByRole("option")).toHaveLength(2);
  fireEvent.keyDown(control, { key: "ArrowDown" });
  fireEvent.keyDown(control, { key: "Enter" });
  expect(change).toHaveBeenCalledWith("en");
  expect(screen.queryByRole("listbox")).toBeNull();
});

it("keeps disabled options unchanged and closes without changing on Escape", () => {
  const change = vi.fn();
  render(
    <Select aria-label="Choice" value="one" onChange={change}>
      <option value="one">One</option>
      <option value="two" disabled>
        Two
      </option>
    </Select>,
  );
  const control = screen.getByRole("combobox");
  fireEvent.click(control);
  fireEvent.click(within(screen.getByRole("listbox")).getByRole("option", { name: "Two" }));
  expect(change).not.toHaveBeenCalled();
  fireEvent.keyDown(control, { key: "Escape" });
  expect(screen.queryByRole("listbox")).toBeNull();
});

it("closes safely when resizing the window while the menu is open", () => {
  render(
    <Select aria-label="Choice" value="one">
      <option value="one">One</option>
    </Select>,
  );
  fireEvent.click(screen.getByRole("combobox"));
  expect(screen.getByRole("listbox")).toBeTruthy();
  fireEvent(window, new Event("resize"));
  expect(screen.queryByRole("listbox")).toBeNull();
});
