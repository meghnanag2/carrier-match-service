// Mirrors the JSON shapes returned by the Go backend's models.go structs.
// Kept in sync by hand — see the README's note on this being a real,
// named maintenance cost of not having codegen from an OpenAPI spec.

export interface Shipper {
  id: string;
  name: string;
  email: string;
}

export interface Carrier {
  id: string;
  name: string;
  address: string;
  lat: number;
  lon: number;
  capacity_lbs: number;
  is_available: boolean;
}

export interface Shipment {
  id: string;
  shipper_id: string;
  origin_address: string;
  origin_lat: number;
  origin_lon: number;
  weight_lbs: number;
  created_at: string;
  status: string;
  carrier_id?: string;
  agreed_price_usd?: number;
  payment_intent_id?: string;
  payment_status?: string;
}

export interface MatchResult {
  carrier_id: string;
  carrier_name: string;
  distance_mi: number;
  feasible: boolean;
  score: number;
  estimated_price_usd: number;
}

export interface ApiError {
  error: string;
}
