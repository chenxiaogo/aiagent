package shutdown

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"time"
)

// Hook 优雅关闭钩子。
type Hook struct {
	signals []os.Signal
	timeout time.Duration
	closers []func(context.Context)
}

// NewHook 创建关闭钩子。
func NewHook() *Hook {
	return &Hook{
		timeout: 30 * time.Second,
	}
}

// WithSignals 设置监听信号。
func (h *Hook) WithSignals(sigs ...os.Signal) *Hook {
	h.signals = sigs
	return h
}

// WithTimeout 设置超时。
func (h *Hook) WithTimeout(d time.Duration) *Hook {
	h.timeout = d
	return h
}

// AddCloseFunc 添加关闭回调。
func (h *Hook) AddCloseFunc(fn func(context.Context)) {
	h.closers = append(h.closers, fn)
}

// Run 启动并等待关闭信号，执行清理。
func (h *Hook) Run(start func(context.Context) error) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := start(ctx); err != nil {
			select {
			case errCh <- err:
			default:
			}
		}
	}()

	sigCh := make(chan os.Signal, 1)
	if len(h.signals) > 0 {
		signal.Notify(sigCh, h.signals...)
	}

	select {
	case err := <-errCh:
		cancel()
		wg.Wait()
		return err
	case <-sigCh:
		cancel()
	}

	cleanCtx, cleanCancel := context.WithTimeout(context.Background(), h.timeout)
	defer cleanCancel()
	for _, fn := range h.closers {
		fn(cleanCtx)
	}
	signal.Stop(sigCh)
	wg.Wait()
	return nil
}