package usecase

import (
	"fmt"
	"io"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
)

type ProxyUseCase struct {
	Log      *logrus.Logger
	SlotUC   *SlotUseCase
	Dialer   SlotDialer
	UserRepo UserRepository
}

func NewProxyUseCase(log *logrus.Logger, slotUC *SlotUseCase, dialer SlotDialer, userRepo UserRepository) *ProxyUseCase {
	return &ProxyUseCase{
		Log:      log,
		SlotUC:   slotUC,
		Dialer:   dialer,
		UserRepo: userRepo,
	}
}

func (c *ProxyUseCase) Authenticate(req model.ProxyAuthRequest) (*model.SlotResponse, error) {
	user, err := c.UserRepo.FindByUsername(req.Username)
	if err != nil {
		return nil, model.ErrInvalidCredentials
	}

	if !user.Enabled {
		return nil, model.ErrUserDisabled
	}

	if user.Password != req.Password {
		return nil, model.ErrInvalidCredentials
	}

	if req.SlotName != "" {
		slot, err := c.SlotUC.SelectByName(req.SlotName)
		if err != nil {
			if c.Log != nil {
				c.Log.Warnf("sticky session failed for %s: %v", req.SlotName, err)
			}
			return nil, fmt.Errorf("%w: %s", model.ErrSlotNotFound, req.SlotName)
		}
		return converter.SlotToResponse(slot), nil
	}

	slot, err := c.SlotUC.SelectRandom()
	if err != nil {
		return nil, model.ErrNoSlotsAvailable
	}
	return converter.SlotToResponse(slot), nil
}

func (c *ProxyUseCase) Connect(slotName string, targetAddr string) (io.ReadWriteCloser, error) {
	c.SlotUC.IncrementConnections(slotName)

	conn, err := c.Dialer.Dial(slotName, targetAddr)
	if err != nil {
		c.SlotUC.DecrementConnections(slotName)
		return nil, fmt.Errorf("dial %s via %s: %w", targetAddr, slotName, err)
	}

	return &trackedConn{
		ReadWriteCloser: conn,
		slotName:        slotName,
		slotUC:          c.SlotUC,
	}, nil
}

type trackedConn struct {
	io.ReadWriteCloser
	slotName string
	slotUC   *SlotUseCase
	closed   bool
}

func (tc *trackedConn) Close() error {
	if !tc.closed {
		tc.closed = true
		tc.slotUC.DecrementConnections(tc.slotName)
	}
	return tc.ReadWriteCloser.Close()
}
