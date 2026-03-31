package model

import "regexp"

type ProxyAuthRequest struct {
	Username string
	Password string
	SlotName string
	ClientIP string
}

var slotSuffixRegex = regexp.MustCompile(`-([a-zA-Z0-9]+_slot\d+)$`)

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
