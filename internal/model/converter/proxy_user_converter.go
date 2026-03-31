package converter

import (
	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
)

func ProxyUserToResponse(user *entity.ProxyUser) *model.ProxyUserResponse {
	return &model.ProxyUserResponse{
		ID:            user.ID,
		Username:      user.Username,
		DeviceBinding: user.DeviceBinding,
		Enabled:       user.Enabled,
	}
}
