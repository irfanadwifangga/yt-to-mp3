import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ApiError } from "./api";
import { t } from "./i18n";
import { Toasts, type Toast } from "./Toasts";

function show(toast: Toast) {
  const onDismiss = vi.fn();
  render(<Toasts toasts={[toast]} onDismiss={onDismiss} />);
  return onDismiss;
}

describe("Toasts", () => {
  it("menutup notifikasi info setelah jedanya habis", () => {
    vi.useFakeTimers();
    const onDismiss = show({ id: "a", tone: "info", title: "Masuk antrean" });

    act(() => vi.advanceTimersByTime(3999));
    expect(onDismiss).not.toHaveBeenCalled();

    act(() => vi.advanceTimersByTime(1));
    expect(onDismiss).toHaveBeenCalledWith("a");
  });

  it("tidak hilang selama disorot", () => {
    vi.useFakeTimers();
    const onDismiss = show({ id: "a", tone: "ok", title: "Selesai" });

    fireEvent.mouseEnter(screen.getByRole("status"));
    act(() => vi.advanceTimersByTime(60_000));
    expect(onDismiss).not.toHaveBeenCalled();

    fireEvent.mouseLeave(screen.getByRole("status"));
    act(() => vi.advanceTimersByTime(12_000));
    expect(onDismiss).toHaveBeenCalledWith("a");
  });

  it("menutup notifikasi setelah tindakannya berhasil", async () => {
    const run = vi.fn().mockResolvedValue(undefined);
    const onDismiss = show({ id: "a", tone: "ok", title: "Selesai", action: { label: "Buka", run } });

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Buka" }));
    });

    expect(run).toHaveBeenCalledOnce();
    expect(onDismiss).toHaveBeenCalledWith("a");
  });

  // Pesan error harus sempat dibaca; notifikasi yang lenyap sendiri
  // menyembunyikan kegagalan.
  it("menampilkan error tindakan dan berhenti menghitung mundur", async () => {
    vi.useFakeTimers();
    const run = vi.fn().mockRejectedValue(new ApiError("INTERNAL", 500));
    const onDismiss = show({ id: "a", tone: "bad", title: "Gagal", action: { label: "Coba", run } });

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Coba" }));
    });
    expect(screen.getByText(t("error.INTERNAL"))).toBeTruthy();

    act(() => vi.advanceTimersByTime(60_000));
    expect(onDismiss).not.toHaveBeenCalled();
  });
});
