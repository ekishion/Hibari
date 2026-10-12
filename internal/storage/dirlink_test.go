package storage

import (
	"testing"
	"time"
)

func TestDirLinkStore_CreateAndResolve(t *testing.T) {
	s := NewDirLinkStore(time.Minute)

	token, err := s.Create("/pictures")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if !DirLinkTokenRe.MatchString(token) {
		t.Fatalf("token format invalid: %q", token)
	}

	dir, ok := s.Resolve(token)
	if !ok || dir != "/pictures" {
		t.Fatalf("Resolve failed: got (%q, %v), want (/pictures, true)", dir, ok)
	}
}

func TestDirLinkStore_Password(t *testing.T) {
	s := NewDirLinkStore(time.Minute)

	token, err := s.CreateWithOptions("/private", "MySecret", 0)
	if err != nil {
		t.Fatalf("CreateWithOptions failed: %v", err)
	}

	// 1. 无密码解析应失败
	dir, ok, pwdReq := s.ResolveWithPassword(token, "")
	if ok || !pwdReq {
		t.Fatalf("expected password required, got ok=%v, pwdReq=%v", ok, pwdReq)
	}

	// 2. 错误密码解析应失败
	dir, ok, pwdReq = s.ResolveWithPassword(token, "wrong")
	if ok || !pwdReq {
		t.Fatalf("expected failure on wrong password")
	}

	// 3. 正确密码大小写不敏感解析应成功
	dir, ok, pwdReq = s.ResolveWithPassword(token, "mysecret")
	if !ok || pwdReq || dir != "/private" {
		t.Fatalf("expected success with correct password, got ok=%v, pwdReq=%v, dir=%q", ok, pwdReq, dir)
	}
}

func TestDirLinkStore_RevokeAndList(t *testing.T) {
	s := NewDirLinkStore(time.Hour)

	token1, _ := s.Create("/folder/docs")
	token2, _ := s.CreateWithOptions("/folder/photos", "123456", 0)

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 active dir links, got %d", len(list))
	}

	// 撤销 token1
	if !s.Revoke(token1) {
		t.Fatalf("expected revoke token1 to succeed")
	}

	// 再次解析 token1 应失效
	if _, ok := s.Resolve(token1); ok {
		t.Fatalf("expected token1 to be resolved false after revoke")
	}

	// list 应只剩 1 个
	list = s.List()
	if len(list) != 1 || list[0].Token != token2 {
		t.Fatalf("expected only token2 in list, got %+v", list)
	}
}
