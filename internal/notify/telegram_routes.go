package notify

import "context"

type TelegramRouteSender struct {
	Telegram *Telegram
}

func (s TelegramRouteSender) Send(_ context.Context, _ Route, text string) error {
	if s.Telegram == nil || !s.Telegram.Enabled() {
		return nil
	}
	s.Telegram.Sendf("%s", Pre(text))
	return nil
}

