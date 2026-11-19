package mail

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Message is a rendered outbound email.
type Message struct {
	Subject  string
	TextBody string
}

// Render builds subject/body for a known outbox template.
func Render(template string, payload []byte) (Message, error) {
	var data map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &data); err != nil {
			return Message{}, fmt.Errorf("payload: %w", err)
		}
	}
	if data == nil {
		data = map[string]any{}
	}
	name := str(data, "first_name")
	switch template {
	case "confirm_email":
		url := str(data, "confirm_url")
		token := str(data, "token")
		body := fmt.Sprintf("Здравствуйте%s!\n\nПодтвердите email для лиги Mr. Pushkin.\n", greet(name))
		if url != "" {
			body += "\nСсылка: " + url + "\n"
		} else if token != "" {
			body += "\nТокен подтверждения: " + token + "\n"
		}
		body += "\nЕсли вы не регистрировались — просто игнорируйте письмо.\n"
		return Message{Subject: "Подтверждение email — Mr. Pushkin", TextBody: body}, nil
	case "registration_received":
		return Message{
			Subject: "Заявка получена — Mr. Pushkin",
			TextBody: fmt.Sprintf(
				"Здравствуйте%s!\n\nМы получили вашу заявку на участие в лиге. Организатор проверит данные и подтвердит аккаунт.\n",
				greet(name),
			),
		}, nil
	case "application_approved":
		level := str(data, "level")
		extra := ""
		if level != "" {
			extra = " Категория: " + level + "."
		}
		return Message{
			Subject: "Заявка одобрена — Mr. Pushkin",
			TextBody: fmt.Sprintf(
				"Здравствуйте%s!\n\nВаша заявка одобрена.%s Можно входить в кабинет и записываться на турниры.\n",
				greet(name), extra,
			),
		}, nil
	case "application_rejected":
		return Message{
			Subject: "Заявка отклонена — Mr. Pushkin",
			TextBody: fmt.Sprintf(
				"Здравствуйте%s!\n\nК сожалению, заявка отклонена. Свяжитесь с организатором лиги, если нужны подробности.\n",
				greet(name),
			),
		}, nil
	case "password_reset":
		url := str(data, "reset_url")
		token := str(data, "token")
		body := fmt.Sprintf("Здравствуйте%s!\n\nЗапрос на сброс пароля в лиге Mr. Pushkin.\n", greet(name))
		if url != "" {
			body += "\nСсылка (действует ограниченное время): " + url + "\n"
		} else if token != "" {
			body += "\nТокен сброса: " + token + "\n"
		}
		body += "\nЕсли вы не запрашивали сброс — игнорируйте письмо.\n"
		return Message{Subject: "Сброс пароля — Mr. Pushkin", TextBody: body}, nil
	default:
		return Message{
			Subject:  "Уведомление — Mr. Pushkin",
			TextBody: fmt.Sprintf("Шаблон: %s\nДанные: %s\n", template, string(payload)),
		}, nil
	}
}

func str(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func greet(firstName string) string {
	if firstName == "" {
		return ""
	}
	return ", " + firstName
}
