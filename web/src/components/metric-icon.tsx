import {
  Activity,
  Beef,
  Bike,
  Droplet,
  Dumbbell,
  Flame,
  Footprints,
  Gauge,
  HeartPulse,
  Moon,
  Mountain,
  Pill,
  Ruler,
  Scale,
  Sun,
  Thermometer,
  Timer,
  Volume2,
  Wheat,
  Wind,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";

// A curated icon per prominent Metric. The long tail (every mineral and vitamin, the
// clinical vitals) falls through to family rules and a generic default in metricIcon,
// so the map stays small and lucide never has to have an icon for "molybdenum".
const EXPLICIT: Record<string, LucideIcon> = {
  // Energy
  active_energy: Flame,
  basal_energy: Flame,
  dietary_energy: Flame,
  total_energy_expenditure: Flame,
  calorie_balance: Scale,
  // Body
  body_mass: Scale,
  lean_body_mass: Scale,
  body_mass_index: Scale,
  body_fat_percentage: Scale,
  height: Ruler,
  // Activity
  steps: Footprints,
  distance_walking_running: Footprints,
  six_minute_walk_test_distance: Footprints,
  distance_cycling: Bike,
  flights_climbed: Mountain,
  physical_effort: Dumbbell,
  apple_exercise_time: Dumbbell,
  apple_stand_time: Timer,
  time_in_daylight: Sun,
  // Respiratory
  vo2_max: Wind,
  respiratory_rate: Wind,
  oxygen_saturation: Droplet,
  // Nutrition macros
  dietary_protein: Beef,
  dietary_carbohydrates: Wheat,
  dietary_fiber: Wheat,
  dietary_water: Droplet,
};

/** metricIcon maps a Catalog slug to a representative lucide icon: an explicit choice
 *  for prominent Metrics, then family rules (nutrition, heart, gait, distance, temp,
 *  audio, sleep), then a generic Activity fallback so every Metric has an icon. */
export function metricIcon(slug: string): LucideIcon {
  const explicit = EXPLICIT[slug];
  if (explicit) return explicit;

  if (slug.startsWith("dietary_fat")) return Droplet;
  if (slug.startsWith("dietary_")) return Pill; // minerals, vitamins → supplement pill
  if (slug.startsWith("heart_rate") || slug.includes("heart_rate")) return HeartPulse;
  if (slug.startsWith("blood_pressure")) return HeartPulse;
  if (slug.startsWith("apple_sleeping")) return Moon;
  if (slug.includes("temperature")) return Thermometer;
  if (slug.includes("audio") || slug.includes("sound")) return Volume2;
  if (slug.startsWith("running_") || slug.startsWith("walking_") || slug.startsWith("stair_")) return Gauge;
  if (slug.startsWith("distance_")) return Footprints;
  if (slug.includes("glucose") || slug.includes("perfusion")) return Droplet;
  return Activity;
}

/** MetricIcon renders a Metric's icon, muted and decorative by default. */
export function MetricIcon({ slug, className }: { slug: string; className?: string }) {
  const Icon = metricIcon(slug);
  return <Icon className={cn("size-4 shrink-0 text-muted-foreground", className)} aria-hidden />;
}
