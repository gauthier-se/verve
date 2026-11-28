package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/gauthier-se/verve/internal/clock"
	"github.com/gauthier-se/verve/internal/store"
	"github.com/gauthier-se/verve/internal/store/sqlite/sqlcgen"
)

// sources implements store.SourcesRepository.
type sources struct {
	q   queries
	clk clock.Clock
}

func (r sources) Resolve(ctx context.Context, userID int64, info store.SourceInfo, kind store.SourceKind,
	seenAt time.Time,
) (store.Source, error) {
	if info.Name == "" || !kind.Known() {
		return store.Source{}, fmt.Errorf("resolve source of kind %q of user %d: %w", kind, userID, store.ErrInvalid)
	}

	var row sqlcgen.Source

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		candidates, err := candidatesOf(ctx, q, userID, info.Name)
		if err != nil {
			return err
		}

		if matched, ok := match(candidates, info.DeviceModel); ok {
			row, err = q.GetUserSource(ctx, sqlcgen.GetUserSourceParams{UserID: userID, ID: matched.sourceID})
			if err != nil {
				return err
			}

			// The identity without model that a record with a model matches takes that model: an
			// alias or a merged source here, and the matched source with its update otherwise.
			takesModel := info.DeviceModel != "" && matched.model == ""
			// The records of the name without model keep matching the source through an alias
			// without model: the one that the identity taking a model leaves behind, or the one
			// that a record without model leaves on the only source of its name.
			leavesAlias := takesModel || matched.onlySource

			if takesModel && matched.alias {
				err := q.SetSourceAliasModel(ctx, sqlcgen.SetSourceAliasModelParams{
					DeviceModel: info.DeviceModel,
					UserID:      userID,
					Name:        info.Name,
				})
				if err != nil {
					return err
				}
			}

			if row.MergedInto.Valid {
				if takesModel && !matched.alias {
					err := q.SetUserSourceModel(ctx, sqlcgen.SetUserSourceModelParams{DeviceModel: info.DeviceModel, UserID: userID, ID: row.ID})
					if err != nil {
						return err
					}
				}

				// A merge in progress makes the merged identity an alias of its target.
				row, err = q.GetUserSource(ctx, sqlcgen.GetUserSourceParams{UserID: userID, ID: row.MergedInto.Int64})
				if err != nil {
					return err
				}

				takesModel = false
			}

			update := seenUpdate(row, info, kind, seenAt)
			if takesModel && !matched.alias {
				update.DeviceModel = info.DeviceModel
			}

			row, err = q.UpdateSource(ctx, update)
			if err != nil || !leavesAlias {
				return err
			}

			return q.CreateSourceAliasWithoutModel(ctx, sqlcgen.CreateSourceAliasWithoutModelParams{
				UserID:   userID,
				Name:     info.Name,
				SourceID: matched.sourceID,
			})
		}

		row, err = q.CreateSource(ctx, sqlcgen.CreateSourceParams{
			UserID:          userID,
			Name:            info.Name,
			DeviceModel:     info.DeviceModel,
			DisplayName:     info.Name,
			BundleID:        info.BundleID,
			DeviceName:      info.DeviceName,
			Manufacturer:    info.Manufacturer,
			HardwareVersion: info.HardwareVersion,
			SoftwareVersion: info.SoftwareVersion,
			Kind:            string(kind),
			FirstSeenAt:     toMillis(seenAt),
			LastSeenAt:      toMillis(seenAt),
		})

		// The only foreign key of a source is its user.
		if isForeignKeyError(err) {
			return store.ErrNotFound
		}

		return constraintError(err)
	})
	if err != nil {
		return store.Source{}, fmt.Errorf("resolve source of user %d: %w", userID, err)
	}

	return sourceOf(row), nil
}

// AddAlias checks the sources for the identity in the transaction of the insert, since no
// constraint spans the two tables.
func (r sources) AddAlias(ctx context.Context, userID int64, identity store.SourceIdentity, sourceID int64) error {
	if identity.Name == "" {
		return fmt.Errorf("add source alias of user %d: name: %w", userID, store.ErrInvalid)
	}

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		_, err := q.GetUserSourceByIdentity(ctx, sqlcgen.GetUserSourceByIdentityParams{
			UserID:      userID,
			Name:        identity.Name,
			DeviceModel: identity.DeviceModel,
		})
		if err == nil {
			return store.ErrConflict
		}

		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		err = q.CreateSourceAlias(ctx, sqlcgen.CreateSourceAliasParams{
			UserID:      userID,
			Name:        identity.Name,
			DeviceModel: identity.DeviceModel,
			SourceID:    sourceID,
		})

		// The foreign keys of an alias are its user and its source of that user.
		if isForeignKeyError(err) {
			return store.ErrNotFound
		}

		return constraintError(err)
	})
	if err != nil {
		return fmt.Errorf("add source alias of user %d to source %d: %w", userID, sourceID, err)
	}

	return nil
}

func (r sources) Get(ctx context.Context, userID, id int64) (store.Source, error) {
	row, err := r.q.read().GetUserSource(ctx, sqlcgen.GetUserSourceParams{UserID: userID, ID: id})
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}

	if err != nil {
		return store.Source{}, fmt.Errorf("get source %d of user %d: %w", id, userID, err)
	}

	return sourceOf(row), nil
}

func (r sources) List(ctx context.Context, userID int64) ([]store.Source, error) {
	rows, err := r.q.read().ListUserSources(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list sources of user %d: %w", userID, err)
	}

	list := make([]store.Source, 0, len(rows))
	for _, row := range rows {
		list = append(list, sourceOf(row))
	}

	return list, nil
}

func (r sources) Rename(ctx context.Context, userID, id int64, displayName string) (store.Source, error) {
	if displayName == "" {
		return store.Source{}, fmt.Errorf("rename source %d of user %d: %w", id, userID, store.ErrInvalid)
	}

	var row sqlcgen.Source

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		row, err = q.RenameUserSource(ctx, sqlcgen.RenameUserSourceParams{DisplayName: displayName, UserID: userID, ID: id})
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrNotFound
		}

		return err
	})
	if err != nil {
		return store.Source{}, fmt.Errorf("rename source %d of user %d: %w", id, userID, err)
	}

	return sourceOf(row), nil
}

func (r sources) SetKind(ctx context.Context, userID, id int64, kind store.SourceKind) (store.Source, error) {
	if !kind.Known() {
		return store.Source{}, fmt.Errorf("set kind of source %d of user %d to %q: %w", id, userID, kind, store.ErrInvalid)
	}

	var row sqlcgen.Source

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		var err error

		row, err = q.SetUserSourceKind(ctx, sqlcgen.SetUserSourceKindParams{Kind: string(kind), UserID: userID, ID: id})
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrNotFound
		}

		return err
	})
	if err != nil {
		return store.Source{}, fmt.Errorf("set kind of source %d of user %d: %w", id, userID, err)
	}

	return sourceOf(row), nil
}

func (r sources) SampleCounts(ctx context.Context, userID int64) (map[int64]int64, error) {
	rows, err := r.q.read().ListUserSourceStats(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("count samples of the sources of user %d: %w", userID, err)
	}

	counts := make(map[int64]int64, len(rows))
	for _, row := range rows {
		counts[row.SourceID] = row.Count
	}

	return counts, nil
}

// StartMerge reads the sources of the user, a few, to check the merge in the transaction of its
// writes.
func (r sources) StartMerge(ctx context.Context, userID, targetID int64, mergedIDs []int64) error {
	ids := append([]int64{targetID}, mergedIDs...)
	if len(mergedIDs) == 0 || len(slices.Compact(slices.Sorted(slices.Values(ids)))) != len(ids) {
		return fmt.Errorf("merge sources of user %d into %d: %w", userID, targetID, store.ErrInvalid)
	}

	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		rows, err := q.ListUserSources(ctx, userID)
		if err != nil {
			return err
		}

		known := make(map[int64]bool, len(rows))
		// inMerge holds the sources of a merge in progress, merged or target.
		inMerge := make(map[int64]bool)

		for _, row := range rows {
			known[row.ID] = true

			if row.MergedInto.Valid {
				inMerge[row.ID] = true
				inMerge[row.MergedInto.Int64] = true
			}
		}

		for _, id := range ids {
			if !known[id] {
				return store.ErrNotFound
			}
		}

		if slices.ContainsFunc(ids, func(id int64) bool { return inMerge[id] }) {
			return store.ErrConflict
		}

		for _, id := range mergedIDs {
			err := q.SetUserSourceMergedInto(ctx, sqlcgen.SetUserSourceMergedIntoParams{
				MergedInto: sql.NullInt64{Int64: targetID, Valid: true},
				UserID:     userID,
				ID:         id,
			})
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("merge sources of user %d into %d: %w", userID, targetID, err)
	}

	return nil
}

// FinishMerge creates the alias once the source is deleted, since no identity is both an alias and
// a source of the same user.
func (r sources) FinishMerge(ctx context.Context, userID, id int64) error {
	err := r.q.write(ctx, func(q *sqlcgen.Queries) error {
		row, err := q.GetUserSource(ctx, sqlcgen.GetUserSourceParams{UserID: userID, ID: id})
		if errors.Is(err, sql.ErrNoRows) {
			return store.ErrNotFound
		}

		if err != nil {
			return err
		}

		if !row.MergedInto.Valid {
			return store.ErrInvalid
		}

		target := row.MergedInto.Int64

		hasRecords, err := q.UserSourceHasRecords(ctx, sqlcgen.UserSourceHasRecordsParams{UserID: userID, SourceID: id})
		if err != nil {
			return err
		}

		if hasRecords == 1 {
			return store.ErrConflict
		}

		if err := q.MoveUserSourceAliases(ctx, sqlcgen.MoveUserSourceAliasesParams{ToID: target, UserID: userID, FromID: id}); err != nil {
			return err
		}

		if err := q.DeleteUserSource(ctx, sqlcgen.DeleteUserSourceParams{UserID: userID, ID: id}); err != nil {
			return err
		}

		return q.CreateSourceAlias(ctx, sqlcgen.CreateSourceAliasParams{
			UserID:      userID,
			Name:        row.Name,
			DeviceModel: row.DeviceModel,
			SourceID:    target,
		})
	})
	if err != nil {
		return fmt.Errorf("finish merge of source %d of user %d: %w", id, userID, err)
	}

	return nil
}

// candidate is an identity of a source or of an alias, of the name of a record, that a record may
// match.
type candidate struct {
	model string
	// sourceID is the source of the identity, or the target of the alias.
	sourceID int64
	alias    bool
	// keepsModel is true for the alias without model that an identity left behind once it took a
	// model, or that a record without model left on the only source of its name: it matches the
	// records without model only.
	keepsModel bool
}

// candidatesOf returns the identities of the sources and of the aliases of userID named name.
func candidatesOf(ctx context.Context, q *sqlcgen.Queries, userID int64, name string) ([]candidate, error) {
	named, err := q.ListUserSourcesByName(ctx, sqlcgen.ListUserSourcesByNameParams{UserID: userID, Name: name})
	if err != nil {
		return nil, err
	}

	aliases, err := q.ListUserSourceAliasesByName(ctx, sqlcgen.ListUserSourceAliasesByNameParams{UserID: userID, Name: name})
	if err != nil {
		return nil, err
	}

	identities := make([]candidate, 0, len(named)+len(aliases))
	for _, source := range named {
		identities = append(identities, candidate{model: source.DeviceModel, sourceID: source.ID})
	}

	for _, alias := range aliases {
		identities = append(identities, candidate{
			model: alias.DeviceModel, sourceID: alias.SourceID, alias: true, keepsModel: alias.TakesModel == 0,
		})
	}

	return identities, nil
}

// matching is the identity that a record matches.
type matching struct {
	candidate
	// onlySource is true for a record without model that matched the only source of its name, of
	// which no identity is without model.
	onlySource bool
}

// match returns the identity among identities, all of one name, that a record of that name and of
// the device model model matches by the rule of FR-2, or false when the record makes a new source.
func match(identities []candidate, model string) (matching, bool) {
	find := func(is func(candidate) bool) (matching, bool) {
		i := slices.IndexFunc(identities, is)
		if i < 0 {
			return matching{}, false
		}

		return matching{candidate: identities[i]}, true
	}

	if model != "" {
		if matched, ok := find(func(id candidate) bool { return id.model == model }); ok {
			return matched, true
		}

		// The identity without model takes the model, unless it is an alias that takes none.
		return find(func(id candidate) bool { return id.model == "" && !id.keepsModel })
	}

	// The identity without model is of the only source of the name when there is one.
	if matched, ok := find(func(id candidate) bool { return id.model == "" }); ok {
		return matched, true
	}

	only := len(identities) > 0 && !slices.ContainsFunc(identities, func(id candidate) bool {
		return id.sourceID != identities[0].sourceID
	})
	if only {
		return matching{candidate: identities[0], onlySource: true}, true
	}

	return matching{}, false
}

// seenUpdate returns the update of the source of row that a record of info and of kind, seen at seenAt,
// matches. The details of info that are not empty, and kind unless it is unknown, replace those of
// the source when seenAt is not before its last seen time, and otherwise only fill the empty ones,
// an unknown kind being empty. A kind set by the user is kept.
func seenUpdate(row sqlcgen.Source, info store.SourceInfo, kind store.SourceKind, seenAt time.Time) sqlcgen.UpdateSourceParams {
	at := toMillis(seenAt)
	latest := at >= row.LastSeenAt

	detail := func(stored, received, empty string) string {
		if received == empty || (stored != empty && !latest) {
			return stored
		}

		return received
	}

	storedKind := row.Kind
	if row.KindSetByUser == 0 {
		storedKind = detail(row.Kind, string(kind), string(store.SourceUnknown))
	}

	return sqlcgen.UpdateSourceParams{
		DeviceModel:     row.DeviceModel,
		BundleID:        detail(row.BundleID, info.BundleID, ""),
		DeviceName:      detail(row.DeviceName, info.DeviceName, ""),
		Manufacturer:    detail(row.Manufacturer, info.Manufacturer, ""),
		HardwareVersion: detail(row.HardwareVersion, info.HardwareVersion, ""),
		SoftwareVersion: detail(row.SoftwareVersion, info.SoftwareVersion, ""),
		Kind:            storedKind,
		LastSeenAt:      max(row.LastSeenAt, at),
		UserID:          row.UserID,
		ID:              row.ID,
	}
}

func sourceOf(row sqlcgen.Source) store.Source {
	return store.Source{
		ID:              row.ID,
		UserID:          row.UserID,
		Name:            row.Name,
		DeviceModel:     row.DeviceModel,
		DisplayName:     row.DisplayName,
		BundleID:        row.BundleID,
		DeviceName:      row.DeviceName,
		Manufacturer:    row.Manufacturer,
		HardwareVersion: row.HardwareVersion,
		SoftwareVersion: row.SoftwareVersion,
		Kind:            store.SourceKind(row.Kind),
		KindSetByUser:   row.KindSetByUser == 1,
		FirstSeenAt:     fromMillis(row.FirstSeenAt),
		LastSeenAt:      fromMillis(row.LastSeenAt),
		MergedInto:      row.MergedInto.Int64,
	}
}
