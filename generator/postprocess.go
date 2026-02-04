package generator

import (
	"errors"
	"regexp"
	"strings"
)

// PostProcess 校验并补全 Draft 基础字段。
func PostProcess(raw string, spec Spec) (Draft, error) {
	md := strings.TrimSpace(raw)
	if md == "" {
		return Draft{}, errors.New("model returned empty markdown")
	}

	title := extractTitle(md)
	digest := extractDigest(md)
	if digest == "" {
		digest = defaultDigest(md, 120)
	}

	return Draft{
		Title:    title,
		Digest:   digest,
		Markdown: md,
	}, nil
}

func extractTitle(md string) string {
	re := regexp.MustCompile(`(?m)^#\s+(.+)$`)
	m := re.FindStringSubmatch(md)
	if len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// 摘要取首段（去掉标题行）。
func extractDigest(md string) string {
	lines := strings.Split(md, "\n")
	var b strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.TrimSpace(line) == "" {
			if b.Len() > 0 {
				break
			}
			continue
		}
		b.WriteString(strings.TrimSpace(line))
		break
	}
	return b.String()
}

func defaultDigest(md string, limit int) string {
	compact := strings.Fields(md)
	joined := strings.Join(compact, " ")
	if len(joined) <= limit {
		return joined
	}
	return joined[:limit]
}
