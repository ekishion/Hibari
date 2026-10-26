package sharing

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ekishion/hibari/internal/sqlite"
)

func newService(t *testing.T) *Service {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "sharing.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(NewSQLStore(db))
}

func mustCreate(t *testing.T, svc *Service, opts CreateOptions) Share {
	t.Helper()
	sh, err := svc.Create(context.Background(), opts)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return sh
}

func TestCreateGeneratesToken(t *testing.T) {
	svc := newService(t)
	sh := mustCreate(t, svc, CreateOptions{Kind: KindFile, Path: "/a.txt", Name: "a.txt", TTL: time.Hour})

	if len(sh.Token) != 32 {
		t.Errorf("令牌长度 = %d，期望 32", len(sh.Token))
	}
	if sh.HasPassword() {
		t.Error("未设提取码时不应有密码散列")
	}
	if sh.ExpiresAt.IsZero() || sh.CreatedAt.IsZero() {
		t.Error("时间字段未填充")
	}
}

func TestCreateRejectsUnknownKind(t *testing.T) {
	svc := newService(t)
	if _, err := svc.Create(context.Background(), CreateOptions{Kind: "symlink", Path: "/a"}); err == nil {
		t.Fatal("未知类型应报错")
	}
}

func TestResolveWithoutPassword(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	sh := mustCreate(t, svc, CreateOptions{Kind: KindFile, Path: "/a.txt", Name: "a.txt", TTL: time.Hour})

	got, err := svc.Resolve(ctx, sh.Token, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Path != "/a.txt" {
		t.Errorf("路径 = %q", got.Path)
	}
}

func TestResolveMissingToken(t *testing.T) {
	svc := newService(t)
	for _, token := range []string{"", "  ", "nosuchtoken"} {
		if _, err := svc.Resolve(context.Background(), token, ""); !errors.Is(err, ErrNotFound) {
			t.Errorf("令牌 %q 期望 ErrNotFound，得到 %v", token, err)
		}
	}
}

func TestResolveRequiresPassword(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	sh := mustCreate(t, svc, CreateOptions{
		Kind: KindFile, Path: "/a.txt", Name: "a.txt", TTL: time.Hour, Password: "1234",
	})

	if _, err := svc.Resolve(ctx, sh.Token, ""); !errors.Is(err, ErrPasswordNeeded) {
		t.Fatalf("期望 ErrPasswordNeeded，得到 %v", err)
	}
	if _, err := svc.Resolve(ctx, sh.Token, "9999"); !errors.Is(err, ErrPasswordWrong) {
		t.Fatalf("期望 ErrPasswordWrong，得到 %v", err)
	}
	if _, err := svc.Resolve(ctx, sh.Token, "1234"); err != nil {
		t.Fatalf("正确提取码应通过: %v", err)
	}
	// 提取码不区分大小写与首尾空格，方便用户输入。
	if _, err := svc.Resolve(ctx, sh.Token, " 1234 "); err != nil {
		t.Fatalf("含空格提取码应通过: %v", err)
	}
}

func TestPasswordHashBoundToToken(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	a := mustCreate(t, svc, CreateOptions{Kind: KindFile, Path: "/a", Name: "a", TTL: time.Hour, Password: "1234"})
	b := mustCreate(t, svc, CreateOptions{Kind: KindFile, Path: "/b", Name: "b", TTL: time.Hour, Password: "1234"})

	if a.PasswordHash == b.PasswordHash {
		t.Fatal("相同提取码在不同分享中应得到不同散列")
	}
	// 提取码不能跨分享复用。
	if _, err := svc.Resolve(ctx, b.Token, "1234"); err != nil {
		t.Fatalf("各自提取码应可用: %v", err)
	}
}

func TestResolveRejectsExpired(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	sh := mustCreate(t, svc, CreateOptions{Kind: KindFile, Path: "/a", Name: "a", TTL: time.Millisecond})
	time.Sleep(5 * time.Millisecond)

	if _, err := svc.Resolve(ctx, sh.Token, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("过期分享期望 ErrNotFound，得到 %v", err)
	}
}

func TestConsumeDecrementsUses(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	sh := mustCreate(t, svc, CreateOptions{
		Kind: KindFile, Path: "/a", Name: "a", TTL: time.Hour, MaxUses: 2,
	})

	for i := 1; i <= 2; i++ {
		got, err := svc.Consume(ctx, sh.Token)
		if err != nil {
			t.Fatalf("第 %d 次消费: %v", i, err)
		}
		if got.Uses != i {
			t.Errorf("第 %d 次后 uses = %d", i, got.Uses)
		}
	}
	if _, err := svc.Consume(ctx, sh.Token); !errors.Is(err, ErrExhausted) {
		t.Fatalf("耗尽后期望 ErrExhausted，得到 %v", err)
	}
	// 次数耗尽后 Resolve 也必须拒绝。
	if _, err := svc.Resolve(ctx, sh.Token, ""); !errors.Is(err, ErrExhausted) {
		t.Fatalf("Resolve 期望 ErrExhausted，得到 %v", err)
	}
}

func TestResolveDoesNotConsume(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	sh := mustCreate(t, svc, CreateOptions{
		Kind: KindFile, Path: "/a", Name: "a", TTL: time.Hour, Password: "1234", MaxUses: 1,
	})

	if _, err := svc.Resolve(ctx, sh.Token, "0000"); !errors.Is(err, ErrPasswordWrong) {
		t.Fatalf("期望 ErrPasswordWrong，得到 %v", err)
	}
	// Resolve 只读，反复调用不应消耗次数。
	for i := 0; i < 3; i++ {
		if _, err := svc.Resolve(ctx, sh.Token, "1234"); err != nil {
			t.Fatalf("第 %d 次 Resolve: %v", i+1, err)
		}
	}
	// 真正扣减一次后即耗尽。
	if _, err := svc.Consume(ctx, sh.Token); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if _, err := svc.Resolve(ctx, sh.Token, "1234"); !errors.Is(err, ErrExhausted) {
		t.Fatalf("耗尽后期望 ErrExhausted，得到 %v", err)
	}
}

func TestUnlimitedUses(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	sh := mustCreate(t, svc, CreateOptions{Kind: KindFile, Path: "/a", Name: "a", TTL: time.Hour})

	for i := 0; i < 3; i++ {
		if _, err := svc.Consume(ctx, sh.Token); err != nil {
			t.Fatalf("无限次分享第 %d 次失败: %v", i+1, err)
		}
	}
}

func TestListFiltersExpired(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	alive := mustCreate(t, svc, CreateOptions{Kind: KindFile, Path: "/a", Name: "a", TTL: time.Hour})
	dead := mustCreate(t, svc, CreateOptions{Kind: KindFile, Path: "/b", Name: "b", TTL: time.Millisecond})
	time.Sleep(5 * time.Millisecond)

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var sawAlive, sawDead bool
	for _, sh := range list {
		if sh.Token == alive.Token {
			sawAlive = true
		}
		if sh.Token == dead.Token {
			sawDead = true
		}
	}
	if !sawAlive || sawDead {
		t.Errorf("列表应只含未过期分享：alive=%v dead=%v", sawAlive, sawDead)
	}
}

func TestRevoke(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)
	sh := mustCreate(t, svc, CreateOptions{Kind: KindFile, Path: "/a", Name: "a", TTL: time.Hour})

	if err := svc.Revoke(ctx, sh.Token); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := svc.Resolve(ctx, sh.Token, ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("撤销后应查不到")
	}
	if err := svc.Revoke(ctx, sh.Token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复撤销期望 ErrNotFound，得到 %v", err)
	}
}
