import type { Carrier, Shipment, Shipper, MatchResult, ApiError } from "./types.js";

const API_BASE = "http://localhost:8080";

class ApiRequestError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = "ApiRequestError";
  }
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });

  if (!response.ok) {
    const body = (await response.json().catch(() => ({ error: response.statusText }))) as ApiError;
    throw new ApiRequestError(response.status, body.error ?? "Request failed");
  }

  return response.json() as Promise<T>;
}

export interface CreateShipperInput {
  name: string;
  email: string;
}

export interface CreateCarrierInput {
  name: string;
  address: string;
  capacity_lbs: number;
}

export interface CreateShipmentInput {
  shipper_id: string;
  origin_address: string;
  weight_lbs: number;
}

export interface DispatchInput {
  carrier_id: string;
  price_usd?: number; // omit to let the backend compute it
}

export const api = {
  createShipper(input: CreateShipperInput): Promise<Shipper> {
    return request<Shipper>("/shippers", {
      method: "POST",
      body: JSON.stringify(input),
    });
  },

  createCarrier(input: CreateCarrierInput): Promise<Carrier> {
    return request<Carrier>("/carriers", {
      method: "POST",
      body: JSON.stringify(input),
    });
  },

  listCarriers(): Promise<Carrier[]> {
    return request<Carrier[]>("/carriers");
  },

  createShipment(input: CreateShipmentInput): Promise<Shipment> {
    return request<Shipment>("/shipments", {
      method: "POST",
      body: JSON.stringify(input),
    });
  },

  getMatches(shipmentId: string): Promise<MatchResult[]> {
    return request<MatchResult[]>(`/shipments/${encodeURIComponent(shipmentId)}/matches`);
  },

  dispatch(shipmentId: string, input: DispatchInput): Promise<Shipment> {
    return request<Shipment>(`/shipments/${encodeURIComponent(shipmentId)}/dispatch`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },

  updateStatus(shipmentId: string, status: string): Promise<Shipment> {
    return request<Shipment>(`/shipments/${encodeURIComponent(shipmentId)}/status`, {
      method: "PATCH",
      body: JSON.stringify({ status }),
    });
  },
};

export { ApiRequestError };
