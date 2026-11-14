package aggregate

import (
	"context"
	"errors"
	"fmt"

	"github.com/gauthier-se/verve/internal/store"
)

// The errors of SetPriorities for priorities it refuses. Their messages are the stable codes that
// the API answers with.
var (
	// ErrSourceUnknown is returned for priorities that hold a source that the user does not have.
	ErrSourceUnknown = errors.New("source_unknown")
	// ErrSourceListedTwice is returned for priorities that hold a source twice, ordered and ignored
	// included.
	ErrSourceListedTwice = errors.New("source_listed_twice")
)

// SourceChange is a change of a source by its user (FR-2 and FR-3 of Deduplication and source
// priority). A nil field is left unchanged.
type SourceChange struct {
	DisplayName *string
	Kind        *store.SourceKind
}

// ChangeSource applies change to the source of userID whose id is sourceID in st, in one
// transaction, and returns the source. A new display name recomputes nothing. A kind is stored as
// set by the user, and when it differs from the kind of the source, every type that the source has
// samples of is recomputed in full with recorder, since the kind places the source in the
// priorities of those types, stored or not. It returns store.ErrNotFound when userID has no source
// of sourceID, and store.ErrInvalid for an empty display name or an unknown kind.
func ChangeSource(ctx context.Context, st store.Store, recorder Recorder, userID, sourceID int64,
	change SourceChange,
) (store.Source, error) {
	var source store.Source

	err := st.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		var err error

		source, err = tx.Sources().Get(ctx, userID, sourceID)
		if err != nil {
			return err
		}

		if change.DisplayName != nil {
			source, err = tx.Sources().Rename(ctx, userID, sourceID, *change.DisplayName)
			if err != nil {
				return err
			}
		}

		if change.Kind == nil {
			return nil
		}

		was := source.Kind

		source, err = tx.Sources().SetKind(ctx, userID, sourceID, *change.Kind)
		if err != nil || source.Kind == was {
			return err
		}

		typeIDs, err := tx.Samples().TypesOfSource(ctx, userID, sourceID)
		if err != nil {
			return err
		}

		return recorder.RecomputeTypes(ctx, tx, userID, typeIDs...)
	})
	if err != nil {
		return store.Source{}, fmt.Errorf("change source %d of user %d: %w", sourceID, userID, err)
	}

	return source, nil
}

// MetricSources returns the resolved priorities of metricType for userID, read in repos: those of
// the sources that have samples of the type or are stored in its priorities, the other sources
// being left out, since they select nothing.
func MetricSources(ctx context.Context, repos store.Repositories, userID int64, metricType store.MetricType) (Priorities, error) {
	stored, err := repos.SourcePriorities().Get(ctx, userID, metricType.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return Priorities{}, fmt.Errorf("get source priorities of type %d: %w", metricType.ID, err)
	}

	withData, err := repos.Samples().SourcesOfType(ctx, userID, metricType.ID)
	if err != nil {
		return Priorities{}, fmt.Errorf("list sources of type %d: %w", metricType.ID, err)
	}

	listed := make(map[int64]bool, len(withData)+len(stored.Order)+len(stored.Ignored))
	for _, ids := range [][]int64{withData, stored.Order, stored.Ignored} {
		for _, id := range ids {
			listed[id] = true
		}
	}

	all, err := repos.Sources().List(ctx, userID)
	if err != nil {
		return Priorities{}, fmt.Errorf("list sources: %w", err)
	}

	sources := make([]store.Source, 0, len(listed))
	for _, source := range all {
		if listed[source.ID] {
			sources = append(sources, source)
		}
	}

	return ResolvePriorities(metricType, sources, stored), nil
}

// SetPriorities stores priorities as the priorities of userID for metricType in st, and recomputes
// the type in full with recorder, in the same transaction (FR-8 of Deduplication and source
// priority), and returns them resolved, as MetricSources does. It returns ErrSourceUnknown for a
// source that userID does not have, ErrSourceListedTwice for a source listed twice, and
// store.ErrCombineNotDiscrete for the combine mode of a type that is not a discrete quantity, and
// then stores nothing.
func SetPriorities(ctx context.Context, st store.Store, recorder Recorder, userID int64, metricType store.MetricType,
	priorities store.SourcePriorities,
) (Priorities, error) {
	var resolved Priorities

	err := st.WithTx(ctx, func(ctx context.Context, tx store.Repositories) error {
		sources, err := tx.Sources().List(ctx, userID)
		if err != nil {
			return err
		}

		if err := checkPriorities(sources, priorities); err != nil {
			return err
		}

		if err := tx.SourcePriorities().Set(ctx, userID, metricType.ID, priorities); err != nil {
			return err
		}

		if err := recorder.RecomputeTypes(ctx, tx, userID, metricType.ID); err != nil {
			return err
		}

		resolved, err = MetricSources(ctx, tx, userID, metricType)

		return err
	})
	if err != nil {
		return Priorities{}, fmt.Errorf("set source priorities of type %d of user %d: %w", metricType.ID, userID, err)
	}

	return resolved, nil
}

// checkPriorities returns ErrSourceUnknown when priorities hold a source that sources, the sources
// of their user, lack, and ErrSourceListedTwice when they hold a source twice.
func checkPriorities(sources []store.Source, priorities store.SourcePriorities) error {
	known := make(map[int64]bool, len(sources))
	for _, source := range sources {
		known[source.ID] = true
	}

	seen := make(map[int64]bool, len(priorities.Order)+len(priorities.Ignored))

	for _, ids := range [][]int64{priorities.Order, priorities.Ignored} {
		for _, id := range ids {
			switch {
			case !known[id]:
				return fmt.Errorf("source %d: %w", id, ErrSourceUnknown)
			case seen[id]:
				return fmt.Errorf("source %d: %w", id, ErrSourceListedTwice)
			}

			seen[id] = true
		}
	}

	return nil
}
