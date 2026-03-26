package proxy

import (
	"regexp"

	"github.com/riakgu/moxy/internal/model"
)

var slotSuffixRegex = regexp.MustCompile(`-slot(\d+)$`)

func ParseProxyAuth(username, password string) model.ProxyAuthRequest {
	req := model.ProxyAuthRequest{
		Username: username,
		Password: password,
	}

	match := slotSuffixRegex.FindStringSubmatchIndex(username)
	if match != nil {
		req.SlotName = username[match[0]+1:]
		req.Username = username[:match[0]]
	}

	return req
}
