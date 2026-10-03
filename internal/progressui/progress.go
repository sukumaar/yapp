package progressui

import (
	"context"
	"fmt"
	"io"

	"github.com/cheggaaa/pb/v3"
)

type Reporter func(message string, current, total int64)

func Run(ctx context.Context, out io.Writer, title string, operation func(Reporter) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "==> "+title); err != nil {
		return err
	}
	var bar *pb.ProgressBar
	lastMessage := ""
	finish := func() {
		if bar != nil {
			bar.Finish()
			bar = nil
		}
	}
	report := func(message string, current, total int64) {
		if total > 0 {
			if bar == nil {
				if message != lastMessage {
					_, _ = fmt.Fprintln(out, "==> "+message)
				}
				bar = pb.New64(total).SetWriter(out).Set(pb.Bytes, true).SetTemplate(`{{counters . }} {{bar . "[" "#" ">" " " "]"}} {{percent . }} {{speed . }} {{rtime . "ETA %s"}}`).SetWidth(24).Start()
			}
			bar.SetCurrent(current)
			return
		}
		finish()
		_, _ = fmt.Fprintln(out, "==> "+message)
		lastMessage = message
	}
	err := operation(report)
	finish()
	if err != nil {
		_, _ = fmt.Fprintf(out, "\n✖ Failed: %v\n", err)
		return err
	}
	_, err = fmt.Fprintln(out, "\n✓ Complete")
	return err
}
