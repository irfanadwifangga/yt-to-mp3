import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { api, ApiError, type Job, type Metadata, type Preset } from "./api";
import { Capture } from "./Capture";
import { t } from "./i18n";

vi.mock("./api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./api")>();
  return {
    ...actual,
    api: { metadata: vi.fn(), createJob: vi.fn(), updateTool: vi.fn(), revealFile: vi.fn() },
  };
});

const LINK = "https://www.youtube.com/watch?v=fJ9rUzIMcZQ";
const OTHER_LINK = "https://www.youtube.com/watch?v=dQw4w9WgXcQ";

const presets: Preset[] = [
  { id: "mp3_standard", label: "Standard", format: "mp3", mode: "cbr", bitrate_kbps: 192, channels: 2 },
  { id: "mp3_high", label: "High", format: "mp3", mode: "cbr", bitrate_kbps: 320, channels: 2 },
];

function metadata(over: Partial<Metadata> = {}): Metadata {
  return {
    source_key: "youtube:fJ9rUzIMcZQ",
    source_url: LINK,
    title: "Queen – Bohemian Rhapsody (Official Video Remastered)",
    uploader: "Queen Official",
    duration_ms: 355_000,
    thumbnail_url: "",
    source_codec: "opus",
    sample_rate: 48_000,
    suggested_title: "Bohemian Rhapsody",
    suggested_artist: "Queen",
    previous_conversions: [],
    ...over,
  };
}

const queuedJob: Job = {
  id: "job_1",
  source_url: LINK,
  source_key: "youtube:fJ9rUzIMcZQ",
  title: "",
  status: "queued",
  preset_id: "mp3_standard",
  filename_mode: "title",
  progress: null,
  attempt_count: 0,
  created_at: "2026-09-13T10:00:00Z",
};

function setup() {
  const onQueued = vi.fn();
  const user = userEvent.setup();
  render(<Capture ready presets={presets} defaultPreset="mp3_standard" onQueued={onQueued} />);
  // Label yang sama juga menamai section-nya, jadi dicari lewat peran.
  const input = screen.getByRole("textbox", { name: t("capture.label") });
  return { onQueued, user, input };
}

/** Janji yang diselesaikan manual, untuk mengatur urutan respons. */
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

const titleField = () => screen.findByLabelText<HTMLInputElement>(t("capture.tagTitle"));
const artistField = () => screen.getByLabelText<HTMLInputElement>(t("capture.tagArtist"));

describe("Capture", () => {
  it("menganalisis tautan lengkap yang diketik, sekali saja", async () => {
    vi.mocked(api.metadata).mockResolvedValue(metadata());
    const { user, input } = setup();

    await user.type(input, LINK);

    await waitFor(() => expect(api.metadata).toHaveBeenCalledWith(LINK), { timeout: 2000 });
    expect(api.metadata).toHaveBeenCalledOnce();
  });

  it("tidak menganalisis tautan yang belum lengkap", async () => {
    const { user, input } = setup();

    await user.type(input, "https://www.youtube.com/watch?v=fJ9r");
    await act(() => new Promise((r) => setTimeout(r, 900)));

    expect(api.metadata).not.toHaveBeenCalled();
  });

  it("mengisi judul dan artis dari saran lalu mengirim suntingannya", async () => {
    vi.mocked(api.metadata).mockResolvedValue(metadata());
    vi.mocked(api.createJob).mockResolvedValue(queuedJob);
    const { user, input, onQueued } = setup();

    await user.click(input);
    await user.paste(LINK);

    const title = await titleField();
    expect(title.value).toBe("Bohemian Rhapsody");
    expect(artistField().value).toBe("Queen");
    // Saran berbeda dari aslinya, jadi judul asli ditampilkan.
    expect(screen.getByText(t("capture.tagOriginal", { title: metadata().title }))).toBeTruthy();

    await user.clear(title);
    await user.type(title, "  Bohemian Rhapsody (Live Aid) ");
    await user.click(screen.getByRole("button", { name: t("capture.convert") }));

    expect(api.createJob).toHaveBeenCalledWith(LINK, "mp3_standard", {
      title: "Bohemian Rhapsody (Live Aid)",
      artist: "Queen",
    });
    expect(onQueued).toHaveBeenCalledWith(expect.objectContaining({ title: "Bohemian Rhapsody (Live Aid)" }));
  });

  it("mengembalikan judul dan artis asli", async () => {
    vi.mocked(api.metadata).mockResolvedValue(metadata());
    const { user, input } = setup();

    await user.click(input);
    await user.paste(LINK);
    await titleField();

    await user.click(screen.getByRole("button", { name: t("capture.tagRestore") }));

    expect((await titleField()).value).toBe(metadata().title);
    expect(artistField().value).toBe(metadata().uploader);
    expect(screen.getByText(t("capture.tagHint"))).toBeTruthy();
  });

  it("memperingatkan konversi ganda hanya untuk preset yang sama", async () => {
    vi.mocked(api.metadata).mockResolvedValue(
      metadata({
        previous_conversions: [
          {
            job_id: "job_lama",
            preset_id: "mp3_standard",
            file_id: "file_1",
            file_name: "Bohemian Rhapsody.mp3",
            finished_at: "2026-09-12T10:00:00Z",
          },
        ],
      }),
    );
    vi.mocked(api.revealFile).mockResolvedValue(undefined);
    const { user, input } = setup();

    await user.click(input);
    await user.paste(LINK);

    expect(await screen.findByRole("button", { name: t("capture.convertAgain") })).toBeTruthy();
    await user.click(screen.getByRole("button", { name: t("history.reveal") }));
    expect(api.revealFile).toHaveBeenCalledWith("file_1");

    await user.selectOptions(screen.getByLabelText(t("capture.preset")), "mp3_high");
    expect(screen.getByRole("button", { name: t("capture.convert") })).toBeTruthy();
    expect(screen.queryByRole("note")).toBeNull();
  });

  it("menawarkan pembaruan yt-dlp saat analisis gagal karena yt-dlp usang", async () => {
    vi.mocked(api.metadata)
      .mockRejectedValueOnce(new ApiError("TOOL_OUTDATED", 422))
      .mockResolvedValueOnce(metadata());
    vi.mocked(api.updateTool).mockResolvedValue({ tools: {}, progress: {} });
    const { user, input } = setup();

    await user.click(input);
    await user.paste(LINK);

    await user.click(await screen.findByRole("button", { name: t("capture.updateYtdlp") }));

    expect(api.updateTool).toHaveBeenCalledWith("yt-dlp");
    expect(await titleField()).toBeTruthy();
    expect(api.metadata).toHaveBeenCalledTimes(2);
  });

  // Respons lambat untuk tautan lama tidak boleh menimpa pratinjau tautan
  // yang ditempel sesudahnya.
  it("membuang hasil analisis untuk tautan yang sudah diganti", async () => {
    const slow = deferred<Metadata>();
    vi.mocked(api.metadata).mockImplementation((link) =>
      link === LINK
        ? slow.promise
        : Promise.resolve(metadata({ source_key: "youtube:dQw4w9WgXcQ", suggested_title: "Never Gonna Give You Up" })),
    );
    const { user, input } = setup();

    await user.click(input);
    await user.paste(LINK);
    await user.clear(input);
    await user.paste(OTHER_LINK);

    expect((await titleField()).value).toBe("Never Gonna Give You Up");

    await act(async () => slow.resolve(metadata()));
    expect((await titleField()).value).toBe("Never Gonna Give You Up");
  });
});
