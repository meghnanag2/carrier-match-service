// Mirrors the JSON shapes returned by the Go backend's models.go structs.
// Kept in sync by hand — if a field is renamed in models.go, it must be
// renamed here too. (A generated-from-OpenAPI approach would remove this
// manual-sync risk; noted as a real improvement in the README, not done
// here to keep the project dependency-free.)

export interface Carrier {
  id: string;
  name: string;
  address: string;
  lat: number;
  lon: number;
  capacity_lbs: number;
}

export interface Shipment {
  id: string;
  origin_address: string;
  origin_lat: number;
  origin_lon: number;
  weight_lbs: number;
  created_at: string;
  status: string;
}

export interface MatchResult {
  carrier_id: string;
  carrier_name: string;
  distance_mi: number;
  feasible: boolean;
  score: number;
}

export interface ApiError {
  error: string;
}
