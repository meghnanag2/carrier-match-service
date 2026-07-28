import { api, ApiRequestError } from "./api.js";
import type { Carrier, MatchResult } from "./types.js";

function requireEl<T extends Element>(selector: string): T {
  const el = document.querySelector<T>(selector);
  if (!el) {
    throw new Error(`Expected element matching "${selector}" to exist in index.html`);
  }
  return el;
}

const shipperForm = requireEl<HTMLFormElement>("#shipper-form");
const shipperStatus = requireEl<HTMLParagraphElement>("#shipper-status");

const carrierForm = requireEl<HTMLFormElement>("#carrier-form");
const carrierList = requireEl<HTMLUListElement>("#carrier-list");
const carrierStatus = requireEl<HTMLParagraphElement>("#carrier-status");

const shipmentForm = requireEl<HTMLFormElement>("#shipment-form");
const shipmentStatus = requireEl<HTMLParagraphElement>("#shipment-status");
const shipmentIdDisplay = requireEl<HTMLParagraphElement>("#shipment-id-display");

const matchForm = requireEl<HTMLFormElement>("#match-form");
const matchResults = requireEl<HTMLDivElement>("#match-results");

const trackingSection = requireEl<HTMLDivElement>("#tracking-section");

let lastShipperId: string | null = null;
let lastShipmentId: string | null = null;

function renderCarrierList(carriers: Carrier[]): void {
  carrierList.innerHTML = "";
  for (const carrier of carriers) {
    const item = document.createElement("li");
    const availability = carrier.is_available ? "available" : "booked";
    item.textContent = `${carrier.name} — ${carrier.address} (capacity: ${carrier.capacity_lbs} lbs, ${availability})`;
    carrierList.appendChild(item);
  }
}

async function refreshCarrierList(): Promise<void> {
  const carriers = await api.listCarriers();
  renderCarrierList(carriers);
}

function renderMatches(shipmentId: string, results: MatchResult[]): void {
  matchResults.innerHTML = "";

  if (results.length === 0) {
    matchResults.textContent = "No carriers registered yet.";
    return;
  }

  const table = document.createElement("table");
  const headerRow = document.createElement("tr");
  for (const heading of ["Carrier", "Distance (mi)", "Feasible", "Score", "Est. Price", "Action"]) {
    const th = document.createElement("th");
    th.textContent = heading;
    headerRow.appendChild(th);
  }
  table.appendChild(headerRow);

  for (const result of results) {
    const row = document.createElement("tr");
    row.className = result.feasible ? "feasible" : "infeasible";

    const cells = [
      result.carrier_name,
      result.distance_mi.toFixed(1),
      result.feasible ? "Yes" : "No",
      result.score.toFixed(1),
      result.feasible ? `$${result.estimated_price_usd.toFixed(2)}` : "—",
    ];
    for (const cellText of cells) {
      const td = document.createElement("td");
      td.textContent = cellText;
      row.appendChild(td);
    }

    const actionTd = document.createElement("td");
    if (result.feasible) {
      const dispatchBtn = document.createElement("button");
      dispatchBtn.textContent = "Dispatch";
      dispatchBtn.addEventListener("click", async () => {
        dispatchBtn.disabled = true;
        dispatchBtn.textContent = "Dispatching…";
        try {
          const shipment = await api.dispatch(shipmentId, { carrier_id: result.carrier_id });
          const paymentNote = shipment.payment_status
            ? ` Payment ${shipment.payment_status} (${shipment.payment_intent_id}).`
            : " (Payment not configured — set STRIPE_SECRET_KEY on the backend to enable it.)";
          matchResults.textContent = `Dispatched to ${result.carrier_name} at $${(shipment.agreed_price_usd ?? 0).toFixed(2)}.${paymentNote}`;
          showTrackingControls(shipmentId);
          await refreshCarrierList(); // that carrier now shows as booked
        } catch (err) {
          dispatchBtn.disabled = false;
          dispatchBtn.textContent = "Dispatch";
          alert(err instanceof ApiRequestError ? err.message : "Dispatch failed unexpectedly.");
        }
      });
      actionTd.appendChild(dispatchBtn);
    }
    row.appendChild(actionTd);

    table.appendChild(row);
  }

  matchResults.appendChild(table);
}

function showTrackingControls(shipmentId: string): void {
  trackingSection.innerHTML = "";
  trackingSection.style.display = "block";

  const label = document.createElement("p");
  label.textContent = `Tracking shipment ${shipmentId}:`;
  trackingSection.appendChild(label);

  for (const status of ["in_transit", "delivered", "cancelled"]) {
    const btn = document.createElement("button");
    btn.textContent = `Mark ${status}`;
    btn.addEventListener("click", async () => {
      try {
        const updated = await api.updateStatus(shipmentId, status);
        label.textContent = `Shipment ${shipmentId} is now: ${updated.status}`;
      } catch (err) {
        alert(err instanceof ApiRequestError ? err.message : "Status update failed unexpectedly.");
      }
    });
    trackingSection.appendChild(btn);
  }
}

shipperForm.addEventListener("submit", async (event: SubmitEvent) => {
  event.preventDefault();
  const formData = new FormData(shipperForm);

  shipperStatus.textContent = "Registering shipper…";
  try {
    const shipper = await api.createShipper({
      name: String(formData.get("name") ?? ""),
      email: String(formData.get("email") ?? ""),
    });
    lastShipperId = shipper.id;
    shipperStatus.textContent = `Shipper registered: ${shipper.name} (ID: ${shipper.id})`;
    shipperForm.reset();
  } catch (err) {
    shipperStatus.textContent = err instanceof ApiRequestError ? `Error: ${err.message}` : "Unexpected error.";
  }
});

carrierForm.addEventListener("submit", async (event: SubmitEvent) => {
  event.preventDefault();
  const formData = new FormData(carrierForm);

  carrierStatus.textContent = "Geocoding address and saving carrier…";
  try {
    await api.createCarrier({
      name: String(formData.get("name") ?? ""),
      address: String(formData.get("address") ?? ""),
      capacity_lbs: Number(formData.get("capacity_lbs") ?? 0),
    });
    carrierStatus.textContent = "Carrier added.";
    carrierForm.reset();
    await refreshCarrierList();
  } catch (err) {
    carrierStatus.textContent = err instanceof ApiRequestError ? `Error: ${err.message}` : "Unexpected error.";
  }
});

shipmentForm.addEventListener("submit", async (event: SubmitEvent) => {
  event.preventDefault();

  if (!lastShipperId) {
    shipmentStatus.textContent = "Register a shipper first (step 1 above).";
    return;
  }

  const formData = new FormData(shipmentForm);

  shipmentStatus.textContent = "Geocoding address and saving shipment…";
  try {
    const shipment = await api.createShipment({
      shipper_id: lastShipperId,
      origin_address: String(formData.get("origin_address") ?? ""),
      weight_lbs: Number(formData.get("weight_lbs") ?? 0),
    });
    lastShipmentId = shipment.id;
    shipmentStatus.textContent = "Shipment added.";
    shipmentIdDisplay.textContent = `Shipment ID: ${shipment.id} (shipper: ${lastShipperId})`;
    shipmentForm.reset();
    trackingSection.style.display = "none";
  } catch (err) {
    shipmentStatus.textContent = err instanceof ApiRequestError ? `Error: ${err.message}` : "Unexpected error.";
  }
});

matchForm.addEventListener("submit", async (event: SubmitEvent) => {
  event.preventDefault();
  const formData = new FormData(matchForm);
  const shipmentId = String(formData.get("shipment_id") ?? "").trim() || lastShipmentId;

  if (!shipmentId) {
    matchResults.textContent = "Enter a shipment ID, or create a shipment above first.";
    return;
  }

  matchResults.textContent = "Finding matches…";
  try {
    const results = await api.getMatches(shipmentId);
    renderMatches(shipmentId, results);
  } catch (err) {
    matchResults.textContent = err instanceof ApiRequestError ? `Error: ${err.message}` : "Unexpected error.";
  }
});

refreshCarrierList().catch(() => {
  carrierStatus.textContent = "Could not reach the backend — is it running on localhost:8080?";
});
