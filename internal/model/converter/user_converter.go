package converter

import (
	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
)

func UserToResponse(user *entity.User) *model.UserResponse {
	return &model.UserResponse{
		Username:      user.Username,
		DeviceBinding: user.DeviceBinding,
		Enabled:       user.Enabled,
	}
}
