import { describe, expect, it } from "vitest";
import type { Job } from "./api";
import { mergeJobs, newlyFinished, pendingIds } from "./jobList";

function job(id: string, createdAt: string, over: Partial<Job> = {}): Job {
  return {
    id,
    source_url: "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
    source_key: "youtube:dQw4w9WgXcQ",
    title: id,
    status: "completed",
    preset_id: "mp3_standard",
    filename_mode: "title",
    progress: 100,
    attempt_count: 0,
    created_at: createdAt,
    ...over,
  };
}

describe("mergeJobs", () => {
  it("membuang duplikat dan memenangkan baris yang lebih baru", () => {
    const old = job("a", "2026-09-13T10:00:00Z", { status: "converting" });
    const fresh = job("a", "2026-09-13T10:00:00Z", { status: "completed" });

    const merged = mergeJobs([fresh], [old]);

    expect(merged).toHaveLength(1);
    expect(merged[0]?.status).toBe("completed");
  });

  // Perbandingan string akan menaruh "10:00:00.5Z" sebelum "10:00:00.25Z".
  it("mengurutkan berdasarkan waktu walau panjang pecahan detik berbeda", () => {
    const merged = mergeJobs(
      [job("a", "2026-09-13T10:00:00.25Z")],
      [job("b", "2026-09-13T10:00:00.5Z"), job("c", "2026-09-13T09:59:59Z")],
    );

    expect(merged.map((j) => j.id)).toEqual(["b", "a", "c"]);
  });

  it("memakai id sebagai penentu untuk waktu yang sama", () => {
    const merged = mergeJobs([job("job_1", "2026-09-13T10:00:00Z")], [job("job_2", "2026-09-13T10:00:00Z")]);

    expect(merged.map((j) => j.id)).toEqual(["job_2", "job_1"]);
  });
});

describe("pengumuman job selesai", () => {
  const previous = new Set(["selesai", "masih", "belum-muncul"]);
  const nowActive = new Set(["masih", "baru"]);
  const finished = [job("selesai", "2026-09-13T10:00:00Z"), job("riwayat-lama", "2026-09-12T10:00:00Z")];

  it("hanya mengumumkan job yang sebelumnya aktif", () => {
    expect(newlyFinished(previous, finished, nowActive).map((j) => j.id)).toEqual(["selesai"]);
  });

  // Daftar aktif dan riwayat diambil pada saat yang sedikit berbeda; job
  // yang sudah tidak aktif tetapi belum masuk riwayat tidak boleh terlupa.
  it("tetap melacak job yang belum muncul di riwayat", () => {
    expect(pendingIds(previous, finished, nowActive)).toEqual(["belum-muncul"]);
  });
});
