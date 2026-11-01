import { expect, it } from "vitest";
import { playlistURL } from "./playlist";
it("accepts a playlist or mixed video link without forwarding arbitrary URLs", () => {
  expect(
    playlistURL("https://www.youtube.com/watch?v=aaaaaaaaaaa&list=PL_test&start_radio=1"),
  ).toBe("https://www.youtube.com/playlist?list=PL_test");
  expect(playlistURL("https://youtube.com/playlist?list=PL_test")).toBe(
    "https://www.youtube.com/playlist?list=PL_test",
  );
  for (const url of [
    "https://evil.test/?list=PL_test",
    "file://youtube.com/playlist?list=PL_test",
    "https://youtube.com/?list=../../x",
    "https://youtube.com/watch?v=aaaaaaaaaaa",
  ])
    expect(playlistURL(url)).toBeNull();
});
