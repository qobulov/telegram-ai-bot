package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"telegram-ai-bot/internal/tools"
)

// locationTimeout leaves the geocoder its 2 s and sending the pin the rest.
const locationTimeout = 8 * time.Second

// Pin is a place to send as a pin on the map, under its name.
type Pin struct {
	ChatID int64
	// ReplyToMessageID makes the pin a reply, which keeps it in a forum
	// topic; zero replies to nothing.
	ReplyToMessageID int64
	Title, Address   string
	Lat, Lon         float64
}

// PinSender sends a pin on the map to a chat.
type PinSender interface {
	SendPin(ctx context.Context, pin Pin) error
}

type locationArgs struct {
	Place string `json:"place"`
}

const pinSentNote = "The pin is in the chat now, under the place's name. Say in one short line which place it is, so a wrong match is visible; don't give the coordinates."

// locationTool sends where a place is as a pin, found the way the weather
// tool finds places: the tables of Uzbek towns, districts and regions and of
// cities abroad, then the Open-Meteo geocoder. Those know settlements and
// areas only, not streets or buildings, so a pin is never a guess at one.
func (a *apis) locationTool() tools.Tool {
	return tools.Tool{
		Name: "send_location",
		Description: "Send a map pin of a town, city, district or region, any spelling, when someone asks where it is or for its location (\"Samarqand qayerda\", \"Xiva lokatsiyasini tashla\", \"где находится Бухара\"). " +
			"Only places with such a name, not streets, shops or buildings. The pin shows its name by itself.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"place": {"type": "string", "description": "The place's name as they wrote it, optionally followed by a comma and its region or country, e.g. Qo'qon, Yangibozor, Xorazm, Paris."}
			},
			"required": ["place"],
			"additionalProperties": false
		}`),
		Timeout: locationTimeout,
		Run:     a.sendLocation,
	}
}

// sendLocation finds the place and sends its pin to the chat the turn is in,
// as a reply in a group, never to a chat the model names.
func (a *apis) sendLocation(ctx context.Context, raw json.RawMessage) (any, error) {
	var args locationArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	place := strings.TrimSpace(args.Place)
	if parsePlace(place).name == "" || len([]rune(place)) > maxCityName {
		return nil, argumentError("place must be a name of at most %d characters", maxCityName)
	}
	caller, ok := tools.CallerFrom(ctx)
	if !ok || caller.ChatID == 0 {
		return nil, errors.New("send_location: no chat to send the pin to")
	}
	where, err := a.findSpot(ctx, place)
	if err != nil {
		return nil, err
	}
	pin := Pin{ChatID: caller.ChatID, Title: where.name, Address: coordinate(where.lat) + ", " + coordinate(where.lon), Lat: where.lat, Lon: where.lon}
	if caller.ChatID < 0 {
		pin.ReplyToMessageID = caller.MessageID
	}
	if err := a.pins.SendPin(ctx, pin); err != nil {
		return nil, &tools.InternalError{Err: err, Message: "the pin could not be sent; say so, and name the place in words"}
	}
	return map[string]any{"sent": true, "place": where.name, "note": pinSentNote}, nil
}
