package identity

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ekishion/hibari/internal/sqlite"
)

func newService(t *testing.T, ttl time.Duration) *Service {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "identity.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(NewSQLStore(db), ttl)
}

func TestNeedsSetupThenCreateAdmin(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, time.Hour)

	needs, err := svc.NeedsSetup(ctx)
	if err != nil {
		t.Fatalf("NeedsSetup: %v", err)
	}
	if !needs {
		t.Fatal("空库应需要初始化")
	}

	u, err := svc.CreateAdmin(ctx, "  admin  ", "correct horse battery")
	if err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	if u.Username != "admin" {
		t.Errorf("用户名未去空格: %q", u.Username)
	}
	if u.Role != RoleAdmin {
		t.Errorf("角色 = %q", u.Role)
	}

	if needs, _ := svc.NeedsSetup(ctx); needs {
		t.Fatal("创建账号后不应再需要初始化")
	}

	// 二次初始化必须被拒，否则并发请求能创建出多个管理员。
	if _, err := svc.CreateAdmin(ctx, "second", "correct horse battery"); !errors.Is(err, ErrAlreadySetup) {
		t.Fatalf("期望 ErrAlreadySetup，得到 %v", err)
	}
}

// TestCreateAdminConcurrent 确认并发初始化只有一个成功。
func TestCreateAdminConcurrent(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, time.Hour)

	const n = 8
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			_, err := svc.CreateAdmin(ctx, "admin", "correct horse battery")
			errs <- err
		}()
	}

	ok := 0
	for i := 0; i < n; i++ {
		if err := <-errs; err == nil {
			ok++
		} else if !errors.Is(err, ErrAlreadySetup) {
			t.Errorf("意外错误: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("成功创建 %d 次，期望 1 次", ok)
	}
}

func TestCreateAdminRejectsWeakPassword(t *testing.T) {
	svc := newService(t, time.Hour)
	if _, err := svc.CreateAdmin(context.Background(), "admin", "short"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("期望 ErrWeakPassword，得到 %v", err)
	}
}

func TestCreateAdminRejectsEmptyUsername(t *testing.T) {
	svc := newService(t, time.Hour)
	if _, err := svc.CreateAdmin(context.Background(), "   ", "correct horse battery"); err == nil {
		t.Fatal("空用户名应报错")
	}
}

func TestLoginAndAuthenticate(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, time.Hour)
	if _, err := svc.CreateAdmin(ctx, "admin", "correct horse battery"); err != nil {
		t.Fatal(err)
	}

	token, user, err := svc.Login(ctx, "admin", "correct horse battery", "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if token == "" || user.Username != "admin" {
		t.Fatalf("令牌或用户异常: %q %+v", token, user)
	}

	got, err := svc.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("用户 ID 不匹配")
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, time.Hour)
	if _, err := svc.CreateAdmin(ctx, "admin", "correct horse battery"); err != nil {
		t.Fatal(err)
	}

	if _, _, err := svc.Login(ctx, "admin", "wrong password", "", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("期望 ErrInvalidCredentials，得到 %v", err)
	}
}

func TestLoginDoesNotRevealMissingUser(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, time.Hour)
	if _, err := svc.CreateAdmin(ctx, "admin", "correct horse battery"); err != nil {
		t.Fatal(err)
	}

	// 用户名不存在与密码错误必须返回同一个错误，避免枚举账号。
	_, _, missing := svc.Login(ctx, "nobody", "whatever", "", "")
	_, _, wrong := svc.Login(ctx, "admin", "whatever", "", "")
	if !errors.Is(missing, ErrInvalidCredentials) || !errors.Is(wrong, ErrInvalidCredentials) {
		t.Fatalf("两种失败应返回同一错误: %v / %v", missing, wrong)
	}
}

func TestAuthenticateRejectsGarbageToken(t *testing.T) {
	svc := newService(t, time.Hour)
	if _, err := svc.Authenticate(context.Background(), "not-a-token"); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("期望 ErrSessionExpired，得到 %v", err)
	}
}

func TestAuthenticateRejectsExpiredSession(t *testing.T) {
	ctx := context.Background()
	// TTL 设为负值会被 New 兜底成 7 天，因此改用极短 TTL 并等待过期。
	svc := newService(t, time.Millisecond)
	if _, err := svc.CreateAdmin(ctx, "admin", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	token, _, err := svc.Login(ctx, "admin", "correct horse battery", "", "")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)

	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("期望 ErrSessionExpired，得到 %v", err)
	}
}

func TestLogoutInvalidatesToken(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, time.Hour)
	if _, err := svc.CreateAdmin(ctx, "admin", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	token, _, err := svc.Login(ctx, "admin", "correct horse battery", "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Logout(ctx, token); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, ErrSessionExpired) {
		t.Fatal("退出后令牌应失效")
	}
	// 空令牌退出不报错，便于幂等调用。
	if err := svc.Logout(ctx, ""); err != nil {
		t.Errorf("空令牌 Logout 应无错: %v", err)
	}
}

func TestChangePasswordRevokesSessions(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, time.Hour)
	user, err := svc.CreateAdmin(ctx, "admin", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := svc.Login(ctx, "admin", "correct horse battery", "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.ChangePassword(ctx, user.ID, "another strong password"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, ErrSessionExpired) {
		t.Fatal("改密后旧会话应失效")
	}
	if _, _, err := svc.Login(ctx, "admin", "another strong password", "", ""); err != nil {
		t.Fatalf("新密码登录失败: %v", err)
	}
	if _, _, err := svc.Login(ctx, "admin", "correct horse battery", "", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("旧密码不应还能登录")
	}
}

func TestChangePasswordRejectsWeak(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, time.Hour)
	user, err := svc.CreateAdmin(ctx, "admin", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ChangePassword(ctx, user.ID, "short"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("期望 ErrWeakPassword，得到 %v", err)
	}
}

func TestVerifyCredentialsDoesNotCreateSession(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, time.Hour)
	if _, err := svc.CreateAdmin(ctx, "admin", "correct horse battery"); err != nil {
		t.Fatal(err)
	}

	u, err := svc.VerifyCredentials(ctx, "admin", "correct horse battery")
	if err != nil {
		t.Fatalf("VerifyCredentials: %v", err)
	}
	if u.Username != "admin" {
		t.Errorf("用户 = %+v", u)
	}
	if _, err := svc.VerifyCredentials(ctx, "admin", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("期望 ErrInvalidCredentials，得到 %v", err)
	}
}

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "correct horse battery" {
		t.Fatal("密码未散列")
	}

	ok, err := VerifyPassword("correct horse battery", hash)
	if err != nil || !ok {
		t.Fatalf("正确密码校验失败: %v %v", ok, err)
	}
	ok, err = VerifyPassword("wrong", hash)
	if err != nil || ok {
		t.Fatalf("错误密码应校验失败: %v %v", ok, err)
	}

	// 同一密码两次散列应得到不同结果（盐不同）。
	hash2, _ := HashPassword("correct horse battery")
	if hash == hash2 {
		t.Fatal("两次散列结果相同，盐未生效")
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	for _, bad := range []string{"", "plain", "$bcrypt$v=1$m=1,t=1,p=1$a$b", "$argon2id$v=19$bad$a$b"} {
		if _, err := VerifyPassword("x", bad); err == nil {
			t.Errorf("散列 %q 应报错", bad)
		}
	}
}

func TestValidatePasswordBounds(t *testing.T) {
	if err := ValidatePassword("1234567"); !errors.Is(err, ErrWeakPassword) {
		t.Error("7 字符应被拒绝")
	}
	if err := ValidatePassword("12345678"); err != nil {
		t.Errorf("8 字符应通过: %v", err)
	}
	if err := ValidatePassword(string(make([]rune, 129))); !errors.Is(err, ErrWeakPassword) {
		t.Error("129 字符应被拒绝")
	}
}
