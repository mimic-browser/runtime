package state

import (
	"mime"
	"regexp"
	"strconv"
	"strings"
)

// MediaContainerPolicy is profile codec policy. Observations do not require a
// decoder device, but all MIME-consuming APIs must agree on the same grammar.
type MediaContainerPolicy struct {
	Codecs        []string
	DefaultCodec  string
	MediaSource   bool
	MediaRecorder bool
}
type MediaFormatCatalog struct {
	Containers     map[string]MediaContainerPolicy
	EfficientVideo []string
}
type MediaTypeSupport struct {
	Play                                           string
	MediaSource, MediaRecorder, Precise, Efficient bool
	Kind                                           string
}

var avcCodec = regexp.MustCompile(`^avc[13]\.([0-9a-fA-F]{6})$`)
var vpCodec = regexp.MustCompile(`^vp09\.(0[0-3])\.([0-9]{2})\.(08|10|12)(\.[0-9]{2}){0,5}$`)
var avCodec = regexp.MustCompile(`^av01\.([0-2])\.([0-9]{2})[MH]\.(08|10|12)(\.[0-9]{1,2}){0,6}$`)
var hevcCodec = regexp.MustCompile(`^(hvc1|hev1)\.[ABC]?[1-3]\.[0-9a-fA-F]+\.[LH][0-9]+(\.[0-9a-fA-F]{1,2})*$`)

func mediaCodec(value string) (family string, complete, alias bool) {
	switch value {
	case "opus", "vorbis", "flac", "mp3":
		return value, true, false
	case "1":
		return "pcm", true, false
	case "vp8":
		return "vp8", true, false
	case "vp8.0":
		return "vp8", true, true
	case "vp9":
		return "vp9", false, false
	case "vp9.0":
		return "vp9", false, true
	case "avc1", "avc3":
		return "avc", false, false
	}
	if m := avcCodec.FindStringSubmatch(value); m != nil {
		profile, _ := strconv.ParseUint(m[1][:2], 16, 8)
		level, _ := strconv.ParseUint(m[1][4:], 16, 8)
		if (profile == 66 || profile == 77 || profile == 88 || profile == 100 || profile == 110 || profile == 122 || profile == 244) && level > 0 && level <= 62 {
			return "avc", true, false
		}
		return "", false, false
	}
	if m := vpCodec.FindStringSubmatch(value); m != nil {
		level, _ := strconv.Atoi(m[2])
		if level >= 10 && level <= 62 {
			return "vp9", true, false
		}
	}
	if m := avCodec.FindStringSubmatch(value); m != nil {
		level, _ := strconv.Atoi(m[2])
		if level <= 23 {
			return "av1", true, false
		}
	}
	if hevcCodec.MatchString(value) {
		return "hevc", true, false
	}
	if strings.HasPrefix(value, "mp4a.") {
		p := strings.Split(value, ".")
		if len(p) == 3 && p[1] == "40" {
			switch p[2] {
			case "2", "5", "29", "42":
				return "aac", true, false
			}
		}
		if len(p) == 2 {
			switch p[1] {
			case "66", "67", "68":
				return "aac", true, false
			case "69", "6b":
				return "mp3", true, false
			}
		}
	}
	return "", false, false
}
func containsMedia(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func (c MediaFormatCatalog) Support(content string) MediaTypeSupport {
	out := MediaTypeSupport{}
	mediaType, p, err := mime.ParseMediaType(content)
	if err != nil {
		return out
	}
	out.Kind = strings.SplitN(mediaType, "/", 2)[0]
	container, ok := c.Containers[mediaType]
	if !ok {
		return out
	}
	codecs := []string{}
	if p["codecs"] != "" {
		for _, v := range strings.Split(p["codecs"], ",") {
			codecs = append(codecs, strings.TrimSpace(v))
		}
	}
	if len(codecs) == 0 {
		if container.DefaultCodec == "" {
			out.Play = "maybe"
			out.MediaRecorder = container.MediaRecorder
			return out
		}
		out.Play = "probably"
		out.Precise = true
		out.MediaSource = container.MediaSource
		out.MediaRecorder = container.MediaRecorder
		out.Efficient = true
		return out
	}
	precise, efficient, mse := true, true, container.MediaSource
	for _, value := range codecs {
		family, complete, alias := mediaCodec(value)
		if family == "" || !containsMedia(container.Codecs, family) {
			return out
		}
		if !complete {
			precise = false
		}
		if alias {
			mse = false
		}
		if family == "avc" && !complete {
			mse = false
		}
		if (family == "vp8" || family == "vp9" || family == "av1" || family == "avc" || family == "hevc") && !containsMedia(c.EfficientVideo, family) {
			efficient = false
		}
	}
	out.Play = "probably"
	if !precise && strings.HasPrefix(codecs[0], "avc") {
		out.Play = "maybe"
	}
	if container.DefaultCodec != "" {
		mse = false
	}
	out.MediaSource = mse
	out.MediaRecorder = container.MediaRecorder
	out.Precise = precise && len(codecs) == 1
	out.Efficient = efficient
	return out
}
