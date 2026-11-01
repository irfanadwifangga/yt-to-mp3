/** Ikon garis sederhana; warnanya mengikuti teks di sekitarnya. */

const common = {
  width: 16,
  height: 16,
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.8,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
  "aria-hidden": true,
};

export function GearIcon() {
  return (
    <svg {...common}>
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" />
    </svg>
  );
}

export function CloseIcon() {
  return (
    <svg {...common}>
      <path d="M18 6 6 18M6 6l12 12" />
    </svg>
  );
}

export function FolderIcon() {
  return (
    <svg {...common}>
      <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
    </svg>
  );
}

export function DownloadIcon() {
  return (
    <svg {...common}>
      <path d="M12 4v11m0 0-4-4m4 4 4-4M5 20h14" />
    </svg>
  );
}

export function MusicIcon() {
  return (
    <svg {...common}>
      <path d="M9 18V5l12-2v13" />
      <circle cx="6" cy="18" r="3" />
      <circle cx="18" cy="16" r="3" />
    </svg>
  );
}

export function VideoIcon() {
  return (
    <svg {...common}>
      <rect x="2" y="6" width="14" height="12" rx="2" />
      <path d="m16 10 6-3v10l-6-3" />
    </svg>
  );
}

export function TerminalIcon() {
  return (
    <svg {...common}>
      <polyline points="4 17 10 11 4 5" />
      <line x1="12" y1="19" x2="20" y2="19" />
    </svg>
  );
}

export function PlayIcon() {
  return (
    <svg {...common}>
      <path d="m8 5 11 7-11 7z" />
    </svg>
  );
}

export function GlobeIcon() {
  return (
    <svg {...common}>
      <circle cx="12" cy="12" r="9" />
      <path d="M3 12h18M12 3a17 17 0 0 1 0 18 17 17 0 0 1 0-18" />
    </svg>
  );
}

export function SearchIcon() {
  return (
    <svg {...common}>
      <circle cx="10.5" cy="10.5" r="6.5" />
      <path d="m16 16 5 5" />
    </svg>
  );
}

export function ScissorsIcon() {
  return (
    <svg {...common}>
      <circle cx="5" cy="6" r="3" />
      <circle cx="5" cy="18" r="3" />
      <path d="m8 8 13 12M8 16 13 11M15 9l6-5" />
    </svg>
  );
}

export function RetryIcon() {
  return (
    <svg {...common}>
      <path d="M3 10a9 9 0 1 1 2 8M3 4v6h6" />
    </svg>
  );
}

export function TrashIcon() {
  return (
    <svg {...common}>
      <path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7M14 10v7" />
    </svg>
  );
}

export function ChevronLeftIcon() {
  return (
    <svg {...common}>
      <path d="m15 5-7 7 7 7" />
    </svg>
  );
}

export function ChevronRightIcon() {
  return (
    <svg {...common}>
      <path d="m9 5 7 7-7 7" />
    </svg>
  );
}

export function PowerIcon() {
  return (
    <svg {...common}>
      <path d="M12 2v10M6.3 5.3a9 9 0 1 0 11.4 0" />
    </svg>
  );
}
