package server

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/ekishion/jikeqingpan/internal/storage"
)

// ===== 状态持久化 =====

// restorePersistedState 启动时从持久化后端恢复短链状态。
// 读取失败（文件损坏等）时把原文件改名留存后重建，服务不因此拒绝启动。
func (s *Server) restorePersistedState() {
	persist, err := storage.NewStatePersistence(s.cfg)
	if err != nil {
		log.Printf("[WARN] 状态持久化初始化失败，本次运行仅内存模式: %v", err)
		return
	}
	if persist == nil {
		return
	}
	st, err := persist.Load()
	if err != nil {
		backup := s.cfg.StatePath + ".corrupt-" + strconv.FormatInt(time.Now().Unix(), 10)
		_ = persist.Close()
		if renameErr := os.Rename(s.cfg.StatePath, backup); renameErr == nil {
			log.Printf("[WARN] 状态文件损坏已留存为 %s: %v", backup, err)
		} else {
			log.Printf("[WARN] 状态文件损坏且无法留存: %v", err)
		}
		persist, err = storage.NewStatePersistence(s.cfg)
		if err != nil || persist == nil {
			return
		}
		st = &storage.PersistedState{Version: 1}
	}
	s.dirLinks.Restore(st.DirLinks)
	s.shortLinks.Restore(st.ShortLinks)
	s.cache.RestoreDirCache(st.DirCache)
	s.persist = persist
	log.Printf("[状态] 已恢复目录短链 %d 条、下载短链 %d 条、目录缓存 %d 个", len(st.DirLinks), len(st.ShortLinks), len(st.DirCache))
}

// startStateFlusher 周期把变脏的状态落盘（30s）。
func (s *Server) startStateFlusher() {
	if s.persist == nil {
		return
	}
	s.flushStop = make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if !s.stateDirty.CompareAndSwap(true, false) {
					continue
				}
				if err := s.flushState(); err != nil {
					log.Printf("[WARN] 状态落盘失败，下个周期重试: %v", err)
					s.stateDirty.Store(true)
				}
			case <-s.flushStop:
				return
			}
		}
	}()
}

// markStateDirty 标记状态有变化，等待周期落盘。
func (s *Server) markStateDirty() {
	if s.persist != nil {
		s.stateDirty.Store(true)
	}
}

// flushState 立即把当前快照写入持久化后端。
func (s *Server) flushState() error {
	if s.persist == nil {
		return nil
	}
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	st := &storage.PersistedState{
		Version:    1,
		DirLinks:   s.dirLinks.Snapshot(),
		ShortLinks: s.shortLinks.Snapshot(),
		DirCache:   s.cache.ExportDirCache(),
	}
	return s.persist.Save(st)
}

// closePersist 优雅关闭时强制落盘并关闭后端；可安全重复调用。
func (s *Server) closePersist() {
	if s.persist == nil {
		return
	}
	if s.flushStop != nil {
		close(s.flushStop)
		s.flushStop = nil
	}
	if err := s.flushState(); err != nil {
		log.Printf("[WARN] 关闭前状态落盘失败: %v", err)
	}
	if err := s.persist.Close(); err != nil {
		log.Printf("[WARN] 关闭持久化后端失败: %v", err)
	}
	s.persist = nil
}
