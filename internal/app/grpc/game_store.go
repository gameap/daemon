package grpc

import (
	"sync"

	"github.com/gameap/daemon/internal/app/domain"
	pb "github.com/gameap/gameap/pkg/proto"
	log "github.com/sirupsen/logrus"
)

type GameStore struct {
	mu       sync.RWMutex
	games    map[string]domain.Game
	gameMods map[uint64]domain.GameMod
}

func NewGameStore() *GameStore {
	return &GameStore{
		games:    make(map[string]domain.Game),
		gameMods: make(map[uint64]domain.GameMod),
	}
}

func (s *GameStore) UpdateGames(games []*pb.Game) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, g := range games {
		s.games[g.Code] = ProtoGameToDomain(g)
	}

	log.WithField("count", len(games)).Debug("Updated games in store")
}

func (s *GameStore) UpdateGameMods(mods []*pb.GameMod) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, m := range mods {
		s.gameMods[m.Id] = ProtoGameModToDomain(m)
	}

	log.WithField("count", len(mods)).Debug("Updated game mods in store")
}

func (s *GameStore) FindGame(code string) (domain.Game, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	g, ok := s.games[code]
	return g, ok
}

func (s *GameStore) FindGameMod(id uint64) (domain.GameMod, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	m, ok := s.gameMods[id]
	return m, ok
}
