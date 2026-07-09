package applehealth

// typeToMetric maps each Apple quantity type to its canonical Catalog slug — data,
// not logic (ADR 0009). A test keeps it in lock-step with the Catalog; absent types
// land in the Unmapped bin (ADR 0002).
var typeToMetric = map[string]string{
	// Energy
	"HKQuantityTypeIdentifierActiveEnergyBurned": "active_energy",
	"HKQuantityTypeIdentifierBasalEnergyBurned":  "basal_energy",

	// Body
	"HKQuantityTypeIdentifierBodyMass":          "body_mass",
	"HKQuantityTypeIdentifierBodyMassIndex":     "body_mass_index",
	"HKQuantityTypeIdentifierBodyFatPercentage": "body_fat_percentage",
	"HKQuantityTypeIdentifierLeanBodyMass":      "lean_body_mass",
	"HKQuantityTypeIdentifierHeight":            "height",

	// Activity
	"HKQuantityTypeIdentifierStepCount":                      "steps",
	"HKQuantityTypeIdentifierDistanceWalkingRunning":         "distance_walking_running",
	"HKQuantityTypeIdentifierDistanceCycling":                "distance_cycling",
	"HKQuantityTypeIdentifierFlightsClimbed":                 "flights_climbed",
	"HKQuantityTypeIdentifierPhysicalEffort":                 "physical_effort",
	"HKQuantityTypeIdentifierAppleExerciseTime":              "apple_exercise_time",
	"HKQuantityTypeIdentifierAppleStandTime":                 "apple_stand_time",
	"HKQuantityTypeIdentifierTimeInDaylight":                 "time_in_daylight",
	"HKQuantityTypeIdentifierWalkingSpeed":                   "walking_speed",
	"HKQuantityTypeIdentifierWalkingStepLength":              "walking_step_length",
	"HKQuantityTypeIdentifierWalkingDoubleSupportPercentage": "walking_double_support_percentage",
	"HKQuantityTypeIdentifierWalkingAsymmetryPercentage":     "walking_asymmetry_percentage",
	"HKQuantityTypeIdentifierAppleWalkingSteadiness":         "walking_steadiness",
	"HKQuantityTypeIdentifierStairAscentSpeed":               "stair_ascent_speed",
	"HKQuantityTypeIdentifierStairDescentSpeed":              "stair_descent_speed",
	"HKQuantityTypeIdentifierSixMinuteWalkTestDistance":      "six_minute_walk_test_distance",
	"HKQuantityTypeIdentifierRunningSpeed":                   "running_speed",
	"HKQuantityTypeIdentifierRunningPower":                   "running_power",
	"HKQuantityTypeIdentifierRunningStrideLength":            "running_stride_length",
	"HKQuantityTypeIdentifierRunningGroundContactTime":       "running_ground_contact_time",
	"HKQuantityTypeIdentifierRunningVerticalOscillation":     "running_vertical_oscillation",

	// Heart & circulation
	"HKQuantityTypeIdentifierHeartRate":                  "heart_rate",
	"HKQuantityTypeIdentifierRestingHeartRate":           "resting_heart_rate",
	"HKQuantityTypeIdentifierWalkingHeartRateAverage":    "walking_heart_rate_average",
	"HKQuantityTypeIdentifierHeartRateVariabilitySDNN":   "heart_rate_variability_sdnn",
	"HKQuantityTypeIdentifierHeartRateRecoveryOneMinute": "heart_rate_recovery_one_minute",
	"HKQuantityTypeIdentifierVO2Max":                     "vo2_max",

	// Respiratory & vitals
	"HKQuantityTypeIdentifierRespiratoryRate":                    "respiratory_rate",
	"HKQuantityTypeIdentifierOxygenSaturation":                   "oxygen_saturation",
	"HKQuantityTypeIdentifierAppleSleepingBreathingDisturbances": "apple_sleeping_breathing_disturbances",
	"HKQuantityTypeIdentifierAppleSleepingWristTemperature":      "apple_sleeping_wrist_temperature",

	// Audio exposure
	"HKQuantityTypeIdentifierHeadphoneAudioExposure":      "headphone_audio_exposure",
	"HKQuantityTypeIdentifierEnvironmentalAudioExposure":  "environmental_audio_exposure",
	"HKQuantityTypeIdentifierEnvironmentalSoundReduction": "environmental_sound_reduction",

	// Nutrition: energy & macros
	"HKQuantityTypeIdentifierDietaryEnergyConsumed":     "dietary_energy",
	"HKQuantityTypeIdentifierDietaryProtein":            "dietary_protein",
	"HKQuantityTypeIdentifierDietaryCarbohydrates":      "dietary_carbohydrates",
	"HKQuantityTypeIdentifierDietaryFatTotal":           "dietary_fat_total",
	"HKQuantityTypeIdentifierDietaryFatSaturated":       "dietary_fat_saturated",
	"HKQuantityTypeIdentifierDietaryFatMonounsaturated": "dietary_fat_monounsaturated",
	"HKQuantityTypeIdentifierDietaryFatPolyunsaturated": "dietary_fat_polyunsaturated",
	"HKQuantityTypeIdentifierDietaryFiber":              "dietary_fiber",
	"HKQuantityTypeIdentifierDietarySugar":              "dietary_sugar",
	"HKQuantityTypeIdentifierDietaryCholesterol":        "dietary_cholesterol",
	"HKQuantityTypeIdentifierDietaryWater":              "dietary_water",

	// Nutrition: minerals
	"HKQuantityTypeIdentifierDietarySodium":     "dietary_sodium",
	"HKQuantityTypeIdentifierDietaryPotassium":  "dietary_potassium",
	"HKQuantityTypeIdentifierDietaryCalcium":    "dietary_calcium",
	"HKQuantityTypeIdentifierDietaryIron":       "dietary_iron",
	"HKQuantityTypeIdentifierDietaryMagnesium":  "dietary_magnesium",
	"HKQuantityTypeIdentifierDietaryPhosphorus": "dietary_phosphorus",
	"HKQuantityTypeIdentifierDietaryZinc":       "dietary_zinc",
	"HKQuantityTypeIdentifierDietaryCopper":     "dietary_copper",
	"HKQuantityTypeIdentifierDietaryManganese":  "dietary_manganese",
	"HKQuantityTypeIdentifierDietarySelenium":   "dietary_selenium",
	"HKQuantityTypeIdentifierDietaryIodine":     "dietary_iodine",
	"HKQuantityTypeIdentifierDietaryChloride":   "dietary_chloride",
	"HKQuantityTypeIdentifierDietaryChromium":   "dietary_chromium",
	"HKQuantityTypeIdentifierDietaryMolybdenum": "dietary_molybdenum",

	// Nutrition: vitamins
	"HKQuantityTypeIdentifierDietaryVitaminA":        "dietary_vitamin_a",
	"HKQuantityTypeIdentifierDietaryVitaminC":        "dietary_vitamin_c",
	"HKQuantityTypeIdentifierDietaryVitaminD":        "dietary_vitamin_d",
	"HKQuantityTypeIdentifierDietaryVitaminE":        "dietary_vitamin_e",
	"HKQuantityTypeIdentifierDietaryVitaminK":        "dietary_vitamin_k",
	"HKQuantityTypeIdentifierDietaryThiamin":         "dietary_thiamin",
	"HKQuantityTypeIdentifierDietaryRiboflavin":      "dietary_riboflavin",
	"HKQuantityTypeIdentifierDietaryNiacin":          "dietary_niacin",
	"HKQuantityTypeIdentifierDietaryVitaminB6":       "dietary_vitamin_b6",
	"HKQuantityTypeIdentifierDietaryVitaminB12":      "dietary_vitamin_b12",
	"HKQuantityTypeIdentifierDietaryFolate":          "dietary_folate",
	"HKQuantityTypeIdentifierDietaryPantothenicAcid": "dietary_pantothenic_acid",
	"HKQuantityTypeIdentifierDietaryBiotin":          "dietary_biotin",

	// Clinical & extended vitals
	"HKQuantityTypeIdentifierBloodPressureSystolic":         "blood_pressure_systolic",
	"HKQuantityTypeIdentifierBloodPressureDiastolic":        "blood_pressure_diastolic",
	"HKQuantityTypeIdentifierBloodGlucose":                  "blood_glucose",
	"HKQuantityTypeIdentifierBodyTemperature":               "body_temperature",
	"HKQuantityTypeIdentifierBasalBodyTemperature":          "basal_body_temperature",
	"HKQuantityTypeIdentifierPeripheralPerfusionIndex":      "peripheral_perfusion_index",
	"HKQuantityTypeIdentifierElectrodermalActivity":         "electrodermal_activity",
	"HKQuantityTypeIdentifierBloodAlcoholContent":           "blood_alcohol_content",
	"HKQuantityTypeIdentifierForcedVitalCapacity":           "forced_vital_capacity",
	"HKQuantityTypeIdentifierForcedExpiratoryVolume1Second": "forced_expiratory_volume_1",
	"HKQuantityTypeIdentifierPeakExpiratoryFlowRate":        "peak_expiratory_flow_rate",

	// Extended activity & body
	"HKQuantityTypeIdentifierDistanceSwimming":           "distance_swimming",
	"HKQuantityTypeIdentifierSwimmingStrokeCount":        "swimming_stroke_count",
	"HKQuantityTypeIdentifierDistanceDownhillSnowSports": "distance_downhill_snow_sports",
	"HKQuantityTypeIdentifierDistanceWheelchair":         "distance_wheelchair",
	"HKQuantityTypeIdentifierPushCount":                  "push_count",
	"HKQuantityTypeIdentifierUVExposure":                 "uv_exposure",
	"HKQuantityTypeIdentifierWaistCircumference":         "waist_circumference",

	// Nutrition (extended)
	"HKQuantityTypeIdentifierDietaryCaffeine": "dietary_caffeine",

	// Events & symptoms (scalar counts)
	"HKQuantityTypeIdentifierInhalerUsage":        "inhaler_usage",
	"HKQuantityTypeIdentifierNumberOfTimesFallen": "number_of_times_fallen",

	// Miscellaneous
	"HKQuantityTypeIdentifierNumberOfAlcoholicBeverages": "number_of_alcoholic_beverages",
}
