package usecase

import (
	"fmt"
	"io"

	"github.com/sirupsen/logrus"

	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/model/converter"
	"github.com/riakgu/moxy/internal/repository"
)

type SlotDialer interface {
	Dial(slotName string, addr string) (io.ReadWriteCloser, error)
}

type ProxyUseCase struct {
	Log          *logrus.Logger
	SlotUC       *SlotUseCase
	Dialer       SlotDialer
	UserRepo     UserRepository
	DestTracker  *repository.DestinationTracker
}

func NewProxyUseCase(log *logrus.Logger, slotUC *SlotUseCase, dialer SlotDialer, userRepo UserRepository) *ProxyUseCase {
	return &ProxyUseCase{
		Log:         log,
		SlotUC:      slotUC,
		Dialer:      dialer,
		UserRepo:    userRepo,
		DestTracker: repository.NewDestinationTracker(1000),
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
			// Slot not found — try on-demand provisioning
			if c.Log != nil {
				c.Log.Infof("slot %s not found, attempting on-demand provisioning", req.SlotName)
			}
			onDemandSlot, provErr := c.SlotUC.ProvisionOnDemand(req.SlotName)
			if provErr != nil {
				if c.Log != nil {
					c.Log.WithError(provErr).Warnf("on-demand provisioning failed for %s", req.SlotName)
				}
				return nil, fmt.Errorf("%w: %s", model.ErrSlotNotFound, req.SlotName)
			}
			return converter.SlotToResponse(onDemandSlot), nil
		}
		return converter.SlotToResponse(slot), nil
	}

	slot, err := c.SlotUC.SelectSlot(req.ClientIP)
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

func (c *ProxyUseCase) AddTraffic(slotName string, bytesSent, bytesReceived int64) {
	c.SlotUC.AddTraffic(slotName, bytesSent, bytesReceived)
}

func (c *ProxyUseCase) RecordDestination(targetAddr string, bytesSent, bytesReceived int64) {
	c.DestTracker.Record(targetAddr, bytesSent, bytesReceived)
}

func (c *ProxyUseCase) GetDestinationStats(limit int) *model.DestinationStatsResponse {
	return c.DestTracker.GetStats(limit)
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
