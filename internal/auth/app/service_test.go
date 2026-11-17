package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"closerai/internal/auth/domain"
	"closerai/internal/kernel"
)

// ---------- фейки портов ----------

type fakeTx struct{}

func (fakeTx) Do(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

type fakeUsers struct{ byID map[uuid.UUID]domain.User }

func (f *fakeUsers) Create(_ context.Context, u domain.User) (domain.User, error) {
	for _, x := range f.byID {
		if x.Email == u.Email {
			return domain.User{}, domain.ErrEmailTaken
		}
	}
	u.ID, u.CreatedAt = uuid.New(), time.Now()
	f.byID[u.ID] = u
	return u, nil
}

func (f *fakeUsers) ByEmail(_ context.Context, email string) (domain.User, error) {
	for _, u := range f.byID {
		if u.Email == email {
			return u, nil
		}
	}
	return domain.User{}, domain.ErrUserNotFound
}

func (f *fakeUsers) ByID(_ context.Context, tenantID, id uuid.UUID) (domain.User, error) {
	u, ok := f.byID[id]
	if !ok || u.TenantID != tenantID {
		return domain.User{}, domain.ErrUserNotFound
	}
	return u, nil
}

func (f *fakeUsers) ListByTenant(_ context.Context, tenantID uuid.UUID) ([]domain.User, error) {
	var out []domain.User
	for _, u := range f.byID {
		if u.TenantID == tenantID {
			out = append(out, u)
		}
	}
	return out, nil
}

func (f *fakeUsers) MarkEmailVerified(_ context.Context, id uuid.UUID) (domain.User, error) {
	u := f.byID[id]
	now := time.Now()
	u.EmailVerifiedAt = &now
	f.byID[id] = u
	return u, nil
}

type fakeCodes struct {
	seq   int64
	codes []domain.EmailCode
}

func (f *fakeCodes) Create(_ context.Context, c domain.EmailCode) (domain.EmailCode, error) {
	f.seq++
	c.ID, c.CreatedAt = f.seq, time.Now()
	f.codes = append(f.codes, c)
	return c, nil
}

func (f *fakeCodes) Latest(_ context.Context, userID uuid.UUID, purpose string) (domain.EmailCode, error) {
	for i := len(f.codes) - 1; i >= 0; i-- {
		if c := f.codes[i]; c.UserID == userID && c.Purpose == purpose {
			return c, nil
		}
	}
	return domain.EmailCode{}, domain.ErrCodeNotFound
}

func (f *fakeCodes) ConsumeAttempt(_ context.Context, id int64, maxAttempts int) error {
	for i := range f.codes {
		if f.codes[i].ID == id {
			if f.codes[i].Attempts >= maxAttempts {
				return domain.ErrCodeAttempts
			}
			f.codes[i].Attempts++
			return nil
		}
	}
	return domain.ErrCodeAttempts
}

func (f *fakeCodes) DeleteAll(_ context.Context, userID uuid.UUID, purpose string) error {
	kept := f.codes[:0]
	for _, c := range f.codes {
		if c.UserID != userID || c.Purpose != purpose {
			kept = append(kept, c)
		}
	}
	f.codes = kept
	return nil
}

type plainHasher struct{}

func (plainHasher) Hash(p string) (string, error) { return "h:" + p, nil }
func (plainHasher) Matches(h, p string) bool      { return h == "h:"+p }

type plainTokens struct{}

func (plainTokens) Issue(kernel.Identity) (string, error) { return "token", nil }
func (plainTokens) Parse(string) (kernel.Identity, error) {
	return kernel.Identity{}, errors.New("n/a")
}

type fixedCodes struct{ code string }

func (f fixedCodes) Generate() (string, error) { return f.code, nil }
func (fixedCodes) Hash(c string) string        { return "c:" + c }
func (fixedCodes) Equal(c, h string) bool      { return h == "c:"+c }

type sentCodes struct{ last string }

func (s *sentCodes) SendVerifyCode(_ context.Context, _ domain.User, code string) error {
	s.last = code
	return nil
}

type env struct {
	svc   *Service
	users *fakeUsers
	codes *fakeCodes
	sent  *sentCodes
}

func newEnv(t *testing.T) env {
	t.Helper()
	e := env{users: &fakeUsers{byID: map[uuid.UUID]domain.User{}}, codes: &fakeCodes{}, sent: &sentCodes{}}
	svc, err := NewService(Deps{
		Tx: fakeTx{}, Users: e.users, Codes: e.codes,
		Tenants: TenantCreatorFunc(func(context.Context, string) (uuid.UUID, error) { return uuid.New(), nil }),
		Hasher:  plainHasher{}, Tokens: plainTokens{}, CodeGen: fixedCodes{"123456"}, Notifier: e.sent,
	})
	require.NoError(t, err)
	e.svc = svc
	return e
}

func (e env) register(t *testing.T) domain.User {
	t.Helper()
	u, _, err := e.svc.Register(context.Background(), RegisterCmd{
		CompanyName: " Acme ", Name: " Ivan ", Email: " Ivan@Example.COM ", Password: "password1",
	})
	require.NoError(t, err)
	return u
}

// ---------- сценарии ----------

func TestRegisterCreatesOwnerAndSendsCode(t *testing.T) {
	e := newEnv(t)
	u := e.register(t)
	require.Equal(t, kernel.RoleOwner, u.Role)
	require.Equal(t, "ivan@example.com", u.Email)
	require.Equal(t, "Ivan", u.Name)
	require.False(t, u.EmailVerified())
	require.Equal(t, "123456", e.sent.last)

	_, _, err := e.svc.Register(context.Background(), RegisterCmd{CompanyName: "B", Name: "x", Email: "ivan@example.com", Password: "password1"})
	require.ErrorIs(t, err, domain.ErrEmailTaken)

	_, _, err = e.svc.Register(context.Background(), RegisterCmd{CompanyName: "   ", Name: "x", Email: "y@example.com", Password: "password1"})
	require.ErrorIs(t, err, domain.ErrEmptyCompanyName)
}

func TestLogin(t *testing.T) {
	e := newEnv(t)
	e.register(t)
	ctx := context.Background()

	_, _, err := e.svc.Login(ctx, LoginCmd{Email: "IVAN@example.com", Password: "password1"})
	require.NoError(t, err)
	_, _, err = e.svc.Login(ctx, LoginCmd{Email: "ivan@example.com", Password: "wrong"})
	require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	_, _, err = e.svc.Login(ctx, LoginCmd{Email: "nobody@example.com", Password: "password1"})
	require.ErrorIs(t, err, domain.ErrInvalidCredentials)
}

func TestVerifyEmail(t *testing.T) {
	e := newEnv(t)
	u := e.register(t)
	ctx := context.Background()

	_, _, err := e.svc.VerifyEmail(ctx, u.Identity(), "000000")
	require.ErrorIs(t, err, domain.ErrCodeInvalid)

	got, _, err := e.svc.VerifyEmail(ctx, u.Identity(), "123456")
	require.NoError(t, err)
	require.True(t, got.EmailVerified())
	require.Empty(t, e.codes.codes, "коды удаляются после подтверждения")

	_, _, err = e.svc.VerifyEmail(ctx, u.Identity(), "123456")
	require.ErrorIs(t, err, domain.ErrAlreadyVerified)
}

func TestVerifyEmailAttemptsLimit(t *testing.T) {
	e := newEnv(t)
	u := e.register(t)
	ctx := context.Background()
	for range domain.CodeMaxAttempts {
		_, _, err := e.svc.VerifyEmail(ctx, u.Identity(), "000000")
		require.ErrorIs(t, err, domain.ErrCodeInvalid)
	}
	// даже верный код после исчерпания попыток не принимается
	_, _, err := e.svc.VerifyEmail(ctx, u.Identity(), "123456")
	require.ErrorIs(t, err, domain.ErrCodeAttempts)
}

func TestResendCooldown(t *testing.T) {
	e := newEnv(t)
	u := e.register(t)
	err := e.svc.ResendVerifyCode(context.Background(), u.Identity())
	var early *domain.ResendTooEarlyError
	require.ErrorAs(t, err, &early)

	e.svc.now = func() time.Time { return time.Now().Add(domain.CodeResendCooldown + time.Second) }
	require.NoError(t, e.svc.ResendVerifyCode(context.Background(), u.Identity()))
}

func TestCreateUserAndTenantIsolation(t *testing.T) {
	e := newEnv(t)
	owner := e.register(t)
	ctx := context.Background()

	op, err := e.svc.CreateUser(ctx, owner.TenantID, CreateUserCmd{Name: "Op", Email: "op@example.com", Password: "password1", Role: kernel.RoleOperator})
	require.NoError(t, err)
	_, err = e.svc.CreateUser(ctx, owner.TenantID, CreateUserCmd{Name: "X", Email: "x@example.com", Password: "password1", Role: "admin"})
	require.ErrorIs(t, err, domain.ErrInvalidRole)

	list, err := e.svc.ListUsers(ctx, owner.TenantID)
	require.NoError(t, err)
	require.Len(t, list, 2)

	_, err = e.svc.Me(ctx, kernel.Identity{UserID: op.ID, TenantID: uuid.New()})
	require.ErrorIs(t, err, domain.ErrUserNotFound)
}
