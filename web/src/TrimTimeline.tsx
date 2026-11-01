import { t } from "./i18n";

function timestamp(ms: number) {
  const value = Math.max(0, Math.round(ms));
  const seconds = Math.floor(value / 1000);
  const fraction = value % 1000;
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}${fraction ? `.${String(fraction).padStart(3, "0")}` : ""}`;
}

export function TrimTimeline({
  durationMS,
  startMS,
  endMS,
  disabled,
  onStart,
  onEnd,
}: {
  durationMS: number;
  startMS: number;
  endMS: number;
  disabled: boolean;
  onStart: (value: number) => void;
  onEnd: (value: number) => void;
}) {
  const start = Math.max(0, Math.min(startMS, durationMS));
  const end = Math.max(start, Math.min(endMS, durationMS));
  return (
    <div className="trim-sliders trim-timeline" role="group" aria-label={t("trim.selection")}>
      <div className="trim-boundaries">
        <span>
          {t("trim.from")} <strong>{timestamp(startMS)}</strong>
        </span>
        <span>
          {t("trim.to")} <strong>{timestamp(endMS)}</strong>
        </span>
      </div>
      <div className="trim-track">
        <span
          className="trim-range-fill"
          aria-hidden="true"
          style={{
            left: `${(start / durationMS) * 100}%`,
            width: `${((end - start) / durationMS) * 100}%`,
          }}>
          <span>{t("trim.selection")}</span>
        </span>
        <input
          aria-label={t("trim.startSlider")}
          aria-valuetext={timestamp(startMS)}
          type="range"
          min="0"
          max={durationMS}
          step="1"
          value={startMS}
          onChange={(e) => onStart(Math.max(0, Math.min(Number(e.target.value), endMS - 1)))}
          disabled={disabled}
        />
        <input
          aria-label={t("trim.endSlider")}
          aria-valuetext={timestamp(endMS)}
          type="range"
          min="0"
          max={durationMS}
          step="1"
          value={endMS}
          onChange={(e) =>
            onEnd(Math.min(durationMS, Math.max(Number(e.target.value), startMS + 1)))
          }
          disabled={disabled}
        />
      </div>
      <div className="trim-axis" aria-hidden="true">
        <span>0:00</span>
        <span>{timestamp(durationMS)}</span>
      </div>
    </div>
  );
}
