package usecase_test

import (
	"testing"

	"github.com/riakgu/moxy/internal/delivery/proxy"
	"github.com/riakgu/moxy/internal/entity"
	"github.com/riakgu/moxy/internal/model"
	"github.com/riakgu/moxy/internal/usecase"
)

// mockUserRepo is a simple in-memory UserRepository for testing.
type mockUserRepo struct {
	users map[string]*entity.User
}

func newMockUserRepo(users ...*entity.User) *mockUserRepo {
	repo := &mockUserRepo{users: make(map[string]*entity.User)}
	for _, u := range users {
		repo.users[u.Username] = u
	}
	return repo
}

func (r *mockUserRepo) FindAll() ([]*entity.User, error) {
	users := make([]*entity.User, 0, len(r.users))
	for _, u := range r.users {
		users = append(users, u)
	}
	return users, nil
}

func (r *mockUserRepo) FindByUsername(username string) (*entity.User, error) {
	u, ok := r.users[username]
	if !ok {
		return nil, model.ErrUserNotFound
	}
	return u, nil
}

func (r *mockUserRepo) Create(user *entity.User) error {
	r.users[user.Username] = user
	return nil
}

func (r *mockUserRepo) Update(user *entity.User) error {
	r.users[user.Username] = user
	return nil
}

func (r *mockUserRepo) Delete(username string) error {
	delete(r.users, username)
	return nil
}

func TestProxyUseCase_Authenticate_ValidRandom(t *testing.T) {
	slotUC := usecase.NewSlotUseCase(nil, nil, nil, nil, "", "")
	slotUC.UpdateSlots([]*model.DiscoveredSlot{
		{Name: "slot0", IPv4Address: "1.1.1.1", Healthy: true},
	})

	repo := newMockUserRepo(&entity.User{Username: "admin", Password: "secret", Enabled: true})
	proxyUC := usecase.NewProxyUseCase(nil, slotUC, nil, repo)

	slot, err := proxyUC.Authenticate(proxy.ParseProxyAuth("admin", "secret"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slot.Name != "slot0" {
		t.Errorf("expected slot0, got %s", slot.Name)
	}
}

func TestProxyUseCase_Authenticate_ValidSticky(t *testing.T) {
	slotUC := usecase.NewSlotUseCase(nil, nil, nil, nil, "", "")
	slotUC.UpdateSlots([]*model.DiscoveredSlot{
		{Name: "slot0", IPv4Address: "1.1.1.1", Healthy: true},
		{Name: "slot3", IPv4Address: "3.3.3.3", Healthy: true},
	})

	repo := newMockUserRepo(&entity.User{Username: "admin", Password: "secret", Enabled: true})
	proxyUC := usecase.NewProxyUseCase(nil, slotUC, nil, repo)

	slot, err := proxyUC.Authenticate(proxy.ParseProxyAuth("admin-slot3", "secret"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slot.Name != "slot3" {
		t.Errorf("expected slot3, got %s", slot.Name)
	}
}

func TestProxyUseCase_Authenticate_WrongPassword(t *testing.T) {
	repo := newMockUserRepo(&entity.User{Username: "admin", Password: "secret", Enabled: true})
	proxyUC := usecase.NewProxyUseCase(nil, nil, nil, repo)

	_, err := proxyUC.Authenticate(proxy.ParseProxyAuth("admin", "wrong"))
	if err == nil {
		t.Fatal("expected auth error for wrong password")
	}
}

func TestProxyUseCase_Authenticate_WrongUsername(t *testing.T) {
	repo := newMockUserRepo(&entity.User{Username: "admin", Password: "secret", Enabled: true})
	proxyUC := usecase.NewProxyUseCase(nil, nil, nil, repo)

	_, err := proxyUC.Authenticate(proxy.ParseProxyAuth("hacker", "secret"))
	if err == nil {
		t.Fatal("expected auth error for wrong username")
	}
}

func TestProxyUseCase_Authenticate_DisabledUser(t *testing.T) {
	repo := newMockUserRepo(&entity.User{Username: "admin", Password: "secret", Enabled: false})
	proxyUC := usecase.NewProxyUseCase(nil, nil, nil, repo)

	_, err := proxyUC.Authenticate(proxy.ParseProxyAuth("admin", "secret"))
	if err == nil {
		t.Fatal("expected auth error for disabled user")
	}
}

func TestProxyUseCase_Authenticate_MultiUser(t *testing.T) {
	slotUC := usecase.NewSlotUseCase(nil, nil, nil, nil, "", "")
	slotUC.UpdateSlots([]*model.DiscoveredSlot{
		{Name: "slot0", IPv4Address: "1.1.1.1", Healthy: true},
	})

	repo := newMockUserRepo(
		&entity.User{Username: "admin", Password: "admin123", Enabled: true},
		&entity.User{Username: "user1", Password: "user123", Enabled: true},
	)
	proxyUC := usecase.NewProxyUseCase(nil, slotUC, nil, repo)

	// Both users should authenticate successfully
	_, err := proxyUC.Authenticate(proxy.ParseProxyAuth("admin", "admin123"))
	if err != nil {
		t.Fatalf("admin auth failed: %v", err)
	}

	_, err = proxyUC.Authenticate(proxy.ParseProxyAuth("user1", "user123"))
	if err != nil {
		t.Fatalf("user1 auth failed: %v", err)
	}

	// Cross-user password should fail
	_, err = proxyUC.Authenticate(proxy.ParseProxyAuth("admin", "user123"))
	if err == nil {
		t.Fatal("expected auth error for cross-user password")
	}
}
