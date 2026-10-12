package storage

import (
	"testing"
	"time"
)

func TestShortLinkStore_CreateAndResolve(t *testing.T) {
	s := NewShortLinkStore(time.Minute, 0)

	token, err := s.Create("/folder/file.pdf")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if !ShortLinkTokenRe.MatchString(token) {
		t.Fatalf("token format invalid: %q", token)
	}

	p, ok := s.Resolve(token, false)
	if !ok || p != "/folder/file.pdf" {
		t.Fatalf("Resolve failed: got (%q, %v), want (/folder/file.pdf, true)", p, ok)
	}
}

func TestShortLinkStore_PasswordProtection(t *testing.T) {
	s := NewShortLinkStore(time.Minute, 0)

	token, err := s.CreateWithOptions("/folder/secret.zip", "Abc123", 0, 0)
	if err != nil {
		t.Fatalf("CreateWithOptions failed: %v", err)
	}

	// 1. 无密码解析应失败且提示需要密码
	p, ok, pwdReq := s.ResolveWithPassword(token, "", false)
	if ok || !pwdReq {
		t.Fatalf("expected password required, got ok=%v, pwdReq=%v", ok, pwdReq)
	}

	// 2. 错误密码解析应失败
	p, ok, pwdReq = s.ResolveWithPassword(token, "wrongpwd", false)
	if ok || !pwdReq {
		t.Fatalf("expected failure on wrong password, got ok=%v, pwdReq=%v", ok, pwdReq)
	}

	// 3. 正确密码大小写不敏感解析应成功
	p, ok, pwdReq = s.ResolveWithPassword(token, "abc123", false)
	if !ok || pwdReq || p != "/folder/secret.zip" {
		t.Fatalf("expected success with lowercase password, got ok=%v, pwdReq=%v, path=%q", ok, pwdReq, p)
	}

	// 4. 元数据检查
	meta, metaOk := s.GetMeta(token)
	if !metaOk {
		t.Fatalf("GetMeta failed")
	}
	if !meta.HasPassword || meta.FileName != "secret.zip" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
}

func TestShortLinkStore_MaxUses(t *testing.T) {
	s := NewShortLinkStore(time.Minute, 0)

	token, err := s.CreateWithOptions("/test/burn.txt", "", 0, 2)
	if err != nil {
		t.Fatalf("CreateWithOptions failed: %v", err)
	}

	// 第一次消费
	p, ok := s.Resolve(token, true)
	if !ok || p != "/test/burn.txt" {
		t.Fatalf("first consume failed")
	}

	// 第二次消费（达到上限并销毁）
	p, ok = s.Resolve(token, true)
	if !ok || p != "/test/burn.txt" {
		t.Fatalf("second consume failed")
	}

	// 第三次消费应返回不存在
	_, ok = s.Resolve(token, true)
	if ok {
		t.Fatalf("expected token to be burned after 2 uses")
	}
}

func TestShortLinkStore_RevokeAndList(t *testing.T) {
	s := NewShortLinkStore(time.Hour, 0)

	token1, _ := s.Create("/folder/a.zip")
	token2, _ := s.CreateWithOptions("/folder/b.zip", "123456", 0, 5)

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 active links, got %d", len(list))
	}

	// 撤销 token1
	if !s.Revoke(token1) {
		t.Fatalf("expected revoke token1 to succeed")
	}

	// 再次解析 token1 应失效
	if _, ok := s.Resolve(token1, false); ok {
		t.Fatalf("expected token1 to be resolved false after revoke")
	}

	// list 应只剩 1 个
	list = s.List()
	if len(list) != 1 || list[0].Token != token2 {
		t.Fatalf("expected only token2 in list, got %+v", list)
	}
}
