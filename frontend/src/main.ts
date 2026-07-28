import { api, ApiRequestError } from "./api.js";
import type { Carrier, MatchResult } from "./types.js";

// Small helper since strict null checks means every DOM query is
// `Element | null` — this asserts it exists and gives a clear error at
// startup if the HTML structure ever drifts from what this script expects,
// instead of a confusing runtime crash deep inside an event handler.
function requireEl<T extends Element>(selector: string): T {
  const el = document.querySelector<T>(selector);
  if (!el) {
    throw new Error(`Expected element matching "${selector}" to exist in index.html`);
  }
  return el;
}

const carrierForm = requireEl<HTMLFormElement>("#carrier-form");
const carrierList = requireEl<HTMLUListElement>("#carrier-list");
const carrierStatus = requireEl<HTMLParagraphElement>("#carrier-status");

const shipmentForm = requireEl<HTMLFormElement>("#shipment-form");
const shipmentStatus = requireEl<HTMLParagraphElement>("#shipment-status");
const shipmentIdDisplay = requireEl<HTMLParagraphElement>("#shipment-id-display");

const matchForm = requireEl<HTMLFormElement>("#match-form");
const matchResults = requireEl<HTMLDivElement>("#match-results");

let lastShipmentId: string | null = null;

function renderCarrierList(carriers: Carrier[]): void {
  carrierList.innerHTML = "";
  for (const carrier of carriers) {
    const item = document.createElement("li");
    item.textContent = `${carrier.name} — ${carrier.address} (capacity: ${carrier.capacity_lbs} lbs)`;
    carrierList.appendChild(item);
  }
}

async function refreshCarrierList(): Promise<void> {
  const carriers = await api.listCarriers();
  renderCarrierList(carriers);
}

function renderMatches(results: MatchResult[]): void {
  matchResults.innerHTML = "";

  if (results.length === 0) {
    matchResults.textContent = "No carriers registered yet.";
    return;
  }

  const table = document.createElement("table");
  const headerRow = document.createElement("tr");
  for (const heading of ["Carrier", "Distance (mi)", "Feasible", "Score"]) {
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
    ];
    for (const cellText of cells) {
      const td = document.createElement("td");
      td.textContent = cellText;
      row.appendChild(td);
    }
    table.appendChild(row);
  }

  matchResults.appendChild(table);
}

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
  const formData = new FormData(shipmentForm);

  shipmentStatus.textContent = "Geocoding address and saving shipment…";
  try {
    const shipment = await api.createShipment({
      origin_address: String(formData.get("origin_address") ?? ""),
      weight_lbs: Number(formData.get("weight_lbs") ?? 0),
    });
    lastShipmentId = shipment.id;
    shipmentStatus.textContent = "Shipment added.";
    shipmentIdDisplay.textContent = `Shipment ID: ${shipment.id}`;
    shipmentForm.reset();
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
    renderMatches(results);
  } catch (err) {
    matchResults.textContent = err instanceof ApiRequestError ? `Error: ${err.message}` : "Unexpected error.";
  }
});

// Populate the carrier list on page load.
refreshCarrierList().catch(() => {
  carrierStatus.textContent = "Could not reach the backend — is it running on localhost:8080?";
});
