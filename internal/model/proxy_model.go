package model

import (
	"regexp"
)

var slotSuffixRegex = regexp.MustCompile(`-slot(\d+)$`)

type ProxyAuthRequest struct {
	Username string
	Password string
	SlotName string
}

func ParseProxyAuth(username, password string) ProxyAuthRequest {
	req := ProxyAuthRequest{
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
