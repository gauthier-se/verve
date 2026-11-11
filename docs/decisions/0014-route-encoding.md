# 0014. Columnar, delta encoded routes compressed with zstd

- **Status:** Accepted
- **Date:** 2026-11-11

## Context

A workout route is stored once per workout as a blob ([Storage and aggregates](../features/data-storage-and-aggregates.md) FR-5). Routes come from the GPX files of the XML export, from Health Auto Export workouts and from the native format, and their points carry time, latitude, longitude, altitude, speed, course, and horizontal and vertical accuracies. A watch records about one point per second, so a few years of running add up to millions of points on a Raspberry Pi. A route is always read whole, for the map of a workout, and the workouts list only needs a thumbnail. Consecutive points differ little, which a generic compressor alone does not exploit on floating point values.

## Decision

- Every field is stored as an integer at a fixed precision, rounded to the nearest unit, halves away from zero:

  | Field | Unit | Presence |
  |---|---|---|
  | Time | millisecond since the Unix epoch, UTC | required |
  | Latitude, longitude | 1e-7 degree | required |
  | Altitude | centimeter | optional |
  | Horizontal accuracy, vertical accuracy | centimeter | optional |
  | Speed | centimeter per second | optional |
  | Course | hundredth of a degree | optional |

- A negative speed, course or accuracy, which Core Location uses for an invalid value, is stored as absent.
- The blob starts with a version byte, `1`, followed by a zstd frame. The frame holds the number of points as an unsigned varint, then one column per field in the order of the table, points in the order received.
- Each column is delta encoded: every value is stored as its difference from the previous value of the same column, the first from zero. Each difference is zigzag encoded and written as an unsigned varint (`encoding/binary.AppendUvarint`).
- An optional column starts with a presence bitmap of one bit per point, least significant bit first, followed by the values of the points that have the field. Deltas are taken between those values.
- Compression uses `github.com/klauspost/compress/zstd`, already used by the [backups](../features/backups.md). A decoder refuses a version byte it does not know.
- The simplified polyline for thumbnails uses Ramer-Douglas-Peucker on the points projected on a local plane around the center of the bounding box, with a tolerance in meters. While the result has more points than a cap, the tolerance doubles. It is stored as a Google encoded polyline. The tolerance and the cap are constants of the code: changing them only affects the routes written afterward.

## Consequences

- A route round-trips exactly at these precisions: decoding gives back every field rounded to its unit, and an absent field stays absent. 1e-7 degree is about 1 cm, below the accuracy of any GPS receiver.
- Points one second apart differ by a few units in most columns, so most deltas fit in one or two bytes before compression, and zstd removes what is left, such as constant time steps and full bitmaps.
- The blob is opaque to SQL. The bounding box and the total distance are stored in their own columns for the queries that need them.
- A change of precision or layout needs a new version byte. Decoders keep reading the versions already stored.

## Alternatives considered

- **GPX or JSON as received**: no encoder to write, but several times larger, and slow to parse on small hardware.
- **One row per point**: queryable, but tens of millions of rows and their index entries for data that is always read whole.
- **Floating point columns with XOR compression (Gorilla)**: compact for time series, but more code, while the precisions of the sources are fixed anyway.
- **Google encoded polyline for the whole route**: compact and standard, but it holds latitude and longitude only, at 1e-5 degree (about 1 m).
- **Protocol Buffers or another schema format**: a dependency and generated code for a layout that fits in one file.
