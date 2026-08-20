import {
  Activity as ActivityIcon,
  Bike,
  Dumbbell,
  Footprints,
  Heart,
  Mountain,
  Snowflake,
  Waves,
  type LucideIcon,
} from "lucide-react";
import type { Activity } from "./types";

// The client half of the Activity display table (ADR 0028). The label, the group
// and the pace/speed reading all come from the server, which owns the curated
// table because the group is a server-side filter. Only the icon lives here, and
// only because it is a React component and cannot cross the wire.
//
// A slug with no icon of its own falls back to its group's, and an unknown group
// to a neutral one. An Activity is never drawn blank.

const EXPLICIT: Record<string, LucideIcon> = {
  running: Footprints,
  walking: Footprints,
  hiking: Mountain,
  cycling: Bike,
  swimming: Waves,
  traditional_strength_training: Dumbbell,
  functional_strength_training: Dumbbell,
  core_training: Dumbbell,
  downhill_skiing: Snowflake,
  cross_country_skiing: Snowflake,
  snowboarding: Snowflake,
  high_intensity_interval_training: Heart,
};

const BY_GROUP: Record<Activity["group"], LucideIcon> = {
  cardio: Heart,
  strength: Dumbbell,
  water: Waves,
  winter: Snowflake,
  other: ActivityIcon,
};

/** activityIcon is the icon for an Activity: its own, else its group's, else a
 *  neutral one. Never undefined, so a list row never renders a hole where an
 *  Activity nobody has curated yet should be. */
export function activityIcon(activity: Activity): LucideIcon {
  return EXPLICIT[activity.slug] ?? BY_GROUP[activity.group] ?? ActivityIcon;
}

/** ACTIVITY_GROUPS is the filter row's order, matching the server's. */
export const ACTIVITY_GROUPS: { value: Activity["group"]; label: string }[] = [
  { value: "cardio", label: "Cardio" },
  { value: "strength", label: "Strength" },
  { value: "water", label: "Water" },
  { value: "winter", label: "Winter" },
  { value: "other", label: "Other" },
];
