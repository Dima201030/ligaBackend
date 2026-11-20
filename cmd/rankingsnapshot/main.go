package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Dima201030/ligaBackend/internal/config"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/rating"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer store.Close()

	opts, err := loadNightlyOpts()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	once := strings.TrimSpace(os.Getenv("RANKING_SNAPSHOT_ONCE")) == "1"
	if once {
		res, err := rating.RunNightly(ctx, store, opts)
		if err != nil {
			log.Fatalf("run: %v", err)
		}
		if res.Skipped {
			log.Printf("done: skipped (%s)", res.SkipReason)
			return
		}
		log.Printf("done: season=%d players=%d points_updated=%d", res.Season, res.Players, res.PointsUpdated)
		return
	}

	if err := rating.RunLoop(ctx, store, opts); err != nil && ctx.Err() == nil {
		log.Fatalf("loop: %v", err)
	}
}

func loadNightlyOpts() (rating.NightlyOptions, error) {
	tzName := strings.TrimSpace(os.Getenv("RANKING_SNAPSHOT_TZ"))
	if tzName == "" {
		tzName = "Europe/Moscow"
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		// Alpine images may lack zoneinfo; Moscow is the product default.
		if tzName == "Europe/Moscow" || tzName == "MSK" {
			loc = time.FixedZone("MSK", 3*3600)
		} else {
			return rating.NightlyOptions{}, err
		}
	}

	hour, minute := 3, 30
	if v := strings.TrimSpace(os.Getenv("RANKING_SNAPSHOT_AT")); v != "" {
		parts := strings.Split(v, ":")
		if len(parts) != 2 {
			return rating.NightlyOptions{}, errInvalidAT(v)
		}
		h, err1 := strconv.Atoi(parts[0])
		m, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
			return rating.NightlyOptions{}, errInvalidAT(v)
		}
		hour, minute = h, m
	}

	var season int32
	if v := strings.TrimSpace(os.Getenv("RANKING_SEASON")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 2000 || n > 2100 {
			return rating.NightlyOptions{}, err
		}
		season = int32(n)
	}

	return rating.NightlyOptions{
		Season:   season,
		Location: loc,
		AtHour:   hour,
		AtMinute: minute,
		Force:    strings.TrimSpace(os.Getenv("RANKING_SNAPSHOT_FORCE")) == "1",
		Log:      log.Default(),
	}, nil
}

type atError string

func (e atError) Error() string { return string(e) }

func errInvalidAT(v string) error {
	return atError("RANKING_SNAPSHOT_AT must be HH:MM, got " + v)
}
