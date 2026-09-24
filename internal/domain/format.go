package domain

// OutputFormat menjelaskan satu wadah keluaran: apa yang boleh disalin dari
// sumber, stream mana yang didahulukan saat mengunduh, dan kemampuan
// wadahnya. Preset memilih format lewat Preset.Format; tabel ini satu-
// satunya tempat pengetahuan per wadah, dipakai bersama oleh pemilihan
// stream yt-dlp dan keputusan salin atau encode FFmpeg. Lihat ADR-036.
type OutputFormat struct {
	Kind PresetKind
	MIME string

	// VideoCopy dan AudioCopy adalah codec sumber (nama ffprobe) yang boleh
	// disalin apa adanya bila preset mengizinkan passthrough. AnyCodec
	// berarti wadahnya menampung codec apa pun.
	VideoCopy []string
	AudioCopy []string

	// PreferVideo dan PreferAudio adalah codec yang didahulukan yt-dlp pada
	// resolusi yang sama, supaya sumbernya sedapat mungkin cukup disalin.
	// Kosong berarti codec terbaik tanpa syarat.
	PreferVideo string
	PreferAudio string

	// Cover berarti sampul dapat disematkan. FastStart berarti wadahnya
	// keluarga MP4, yang indeksnya dapat dipindah ke awal berkas.
	Cover     bool
	FastStart bool
}

// AnyCodec menandai wadah yang menampung codec apa pun.
var AnyCodec = []string{"*"}

var outputFormats = map[string]OutputFormat{
	// Audio. Sampul hanya untuk wadah yang pemutarnya membacanya dengan
	// andal; Ogg dan WAV tidak.
	"mp3":  {Kind: KindAudio, MIME: "audio/mpeg", Cover: true},
	"m4a":  {Kind: KindAudio, MIME: "audio/mp4", Cover: true, FastStart: true},
	"opus": {Kind: KindAudio, MIME: "audio/ogg", AudioCopy: []string{"opus"}},
	"ogg":  {Kind: KindAudio, MIME: "audio/ogg"},
	"flac": {Kind: KindAudio, MIME: "audio/flac", Cover: true},
	"wav":  {Kind: KindAudio, MIME: "audio/wav"},

	// Video. MP4, MOV, dan FLV adalah H.264 + AAC yang diputar di mana
	// saja; WebM adalah VP9/AV1 + Opus untuk web; MKV menyimpan sumber apa
	// adanya sampai 4K tanpa encode ulang; AVI adalah MPEG-4 Part 2 (Xvid)
	// + MP3 untuk pemutar lama, jadi selalu di-encode.
	"mp4": {
		Kind: KindVideo, MIME: "video/mp4", FastStart: true,
		VideoCopy: []string{"h264"}, AudioCopy: []string{"aac"},
		PreferVideo: "h264", PreferAudio: "aac",
	},
	"mov": {
		Kind: KindVideo, MIME: "video/quicktime", FastStart: true,
		VideoCopy: []string{"h264"}, AudioCopy: []string{"aac"},
		PreferVideo: "h264", PreferAudio: "aac",
	},
	"flv": {
		Kind: KindVideo, MIME: "video/x-flv",
		VideoCopy: []string{"h264"}, AudioCopy: []string{"aac"},
		PreferVideo: "h264", PreferAudio: "aac",
	},
	"webm": {
		Kind: KindVideo, MIME: "video/webm",
		VideoCopy: []string{"vp9", "vp8", "av1"}, AudioCopy: []string{"opus", "vorbis"},
		PreferVideo: "vp9", PreferAudio: "opus",
	},
	"mkv": {
		Kind: KindVideo, MIME: "video/x-matroska",
		VideoCopy: AnyCodec, AudioCopy: AnyCodec,
	},
	"avi": {
		Kind: KindVideo, MIME: "video/x-msvideo",
		VideoCopy: []string{"mpeg4"}, AudioCopy: []string{"mp3"},
		// Videonya pasti di-encode ulang; H.264 paling murah di-decode.
		PreferVideo: "h264",
	},
}

// FormatOf mengembalikan profil sebuah format keluaran.
func FormatOf(format string) (OutputFormat, bool) {
	f, ok := outputFormats[format]
	return f, ok
}

// Copies melaporkan apakah codec sumber boleh disalin ke wadah dengan
// daftar codec tersebut.
func Copies(allowed []string, codec string) bool {
	for _, c := range allowed {
		if c == "*" || c == codec {
			return codec != ""
		}
	}
	return false
}
