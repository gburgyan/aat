<script lang="ts">
  // FlightSearchVisualizer — renders CatalogProductOfferingsResponse as itinerary cards.

  interface Props {
    body: unknown;
  }

  let { body }: Props = $props();

  // --- Types ---

  interface FlightRef {
    id: string;
    carrier: string;
    number: string;
    duration: string;
    equipment: string;
    distance?: number;
    Departure: { location: string; date: string; time: string; terminal?: string };
    Arrival: { location: string; date: string; time: string; terminal?: string };
  }

  interface BrandRef {
    id: string;
    name: string;
    tier: number;
    code: string;
    ImageURL?: string[];
    BrandAttribute?: { classification: string; inclusion: string }[];
  }

  interface ProductRef {
    id: string;
    totalDuration: string;
    PassengerFlight?: {
      FlightProduct?: {
        cabin?: string;
        fareBasisCode?: string;
        classOfService?: string;
        fareType?: string;
      }[];
    }[];
  }

  interface PriceInfo {
    currency: string;
    base: number;
    taxes: number;
    fees: number;
    total: number;
  }

  interface FareOption {
    brandName: string;
    brandTier: number;
    brandImageUrl: string;
    price: PriceInfo;
    cabin: string;
    fareBasis: string;
    classOfService: string;
    fareType: string;
    amenities: { name: string; inclusion: string }[];
  }

  interface FlightSegment {
    carrier: string;
    flightNumber: string;
    origin: string;
    destination: string;
    departureDate: string;
    departureTime: string;
    arrivalTime: string;
    arrivalDate: string;
    duration: string;
    equipment: string;
    departureTerminal: string;
    arrivalTerminal: string;
  }

  interface ItineraryCard {
    id: string;
    sequence: number;
    origin: string;
    destination: string;
    segments: FlightSegment[];
    fareOptions: FareOption[];
    cheapestPrice: number;
    currency: string;
    primaryCarrier: string;
    totalDuration: string;
  }

  type SortMode = 'price' | 'departure' | 'duration';

  // --- State ---

  let sortMode = $state<SortMode>('price');
  let expandedCards = $state<Set<string>>(new Set());

  // --- Parsing ---

  function parse(body: unknown): ItineraryCard[] {
    if (!body || typeof body !== 'object') return [];
    const outer = body as Record<string, unknown>;
    const resp = outer['CatalogProductOfferingsResponse'] as Record<string, unknown> | undefined;
    if (!resp) return [];

    const flightsById = new Map<string, FlightRef>();
    const brandsById = new Map<string, BrandRef>();
    const productsById = new Map<string, ProductRef>();

    const refLists = (resp['ReferenceList'] ?? []) as Record<string, unknown>[];
    for (const ref of refLists) {
      const t = ref['@type'] as string;
      if (t === 'ReferenceListFlight') {
        for (const f of (ref['Flight'] ?? []) as FlightRef[]) {
          flightsById.set(f.id, f);
        }
      } else if (t === 'ReferenceListBrand') {
        for (const b of (ref['Brand'] ?? []) as BrandRef[]) {
          brandsById.set(b.id, b);
        }
      } else if (t === 'ReferenceListProduct') {
        for (const p of (ref['Product'] ?? []) as ProductRef[]) {
          productsById.set(p.id, p);
        }
      }
    }

    const cpos = resp['CatalogProductOfferings'] as Record<string, unknown> | undefined;
    if (!cpos) return [];
    const offerings = (cpos['CatalogProductOffering'] ?? []) as Record<string, unknown>[];

    const cards: ItineraryCard[] = [];

    for (const offering of offerings) {
      const id = offering['id'] as string;
      const sequence = offering['sequence'] as number;
      const origin = offering['Departure'] as string;
      const destination = offering['Arrival'] as string;

      const options = (offering['ProductBrandOptions'] ?? []) as Record<string, unknown>[];

      let headerSegments: FlightSegment[] = [];
      const allFareOptions: FareOption[] = [];
      let primaryCarrier = '';
      let totalDuration = '';

      for (const opt of options) {
        const fRefs = (opt['flightRefs'] ?? []) as string[];
        const segments = fRefs.map(ref => {
          const f = flightsById.get(ref);
          if (!f) return null;
          return {
            carrier: f.carrier,
            flightNumber: `${f.carrier}${f.number}`,
            origin: f.Departure.location,
            destination: f.Arrival.location,
            departureDate: f.Departure.date,
            departureTime: formatTime(f.Departure.time),
            arrivalTime: formatTime(f.Arrival.time),
            arrivalDate: f.Arrival.date,
            duration: formatDuration(f.duration),
            equipment: f.equipment,
            departureTerminal: f.Departure.terminal ?? '',
            arrivalTerminal: f.Arrival.terminal ?? '',
          };
        }).filter(Boolean) as FlightSegment[];

        if (headerSegments.length === 0 && segments.length > 0) {
          headerSegments = segments;
          primaryCarrier = segments[0].carrier;
        }

        const pbos = (opt['ProductBrandOffering'] ?? []) as Record<string, unknown>[];
        for (const pbo of pbos) {
          const brandId = (pbo['Brand'] as Record<string, unknown>)?.['BrandRef'] as string ?? '';
          const brand = brandsById.get(brandId);
          const price = pbo['BestCombinablePrice'] as Record<string, unknown> | undefined;

          const productIds = ((pbo['Product'] ?? []) as Record<string, unknown>[])
            .map(p => p['productRef'] as string).filter(Boolean);
          let cabin = '';
          let fareBasis = '';
          let classOfService = '';
          let fareType = '';
          for (const pid of productIds) {
            const prod = productsById.get(pid);
            if (!prod) continue;
            if (!totalDuration && prod.totalDuration) totalDuration = prod.totalDuration;
            for (const pf of prod.PassengerFlight ?? []) {
              for (const fp of pf.FlightProduct ?? []) {
                if (!cabin && fp.cabin) cabin = fp.cabin;
                if (!fareBasis && fp.fareBasisCode) fareBasis = fp.fareBasisCode;
                if (!classOfService && fp.classOfService) classOfService = fp.classOfService;
                if (!fareType && fp.fareType) fareType = fp.fareType;
              }
            }
          }

          const amenities: { name: string; inclusion: string }[] = [];
          if (brand?.BrandAttribute) {
            for (const attr of brand.BrandAttribute) {
              amenities.push({ name: attr.classification, inclusion: attr.inclusion });
            }
          }

          const currCode = price?.['CurrencyCode'] as Record<string, unknown> | undefined;
          allFareOptions.push({
            brandName: brand?.name ?? brandId,
            brandTier: brand?.tier ?? 0,
            brandImageUrl: brand?.ImageURL?.[0] ?? '',
            price: {
              currency: (currCode?.['value'] as string) ?? '',
              base: (price?.['Base'] as number) ?? 0,
              taxes: (price?.['TotalTaxes'] as number) ?? 0,
              fees: (price?.['TotalFees'] as number) ?? 0,
              total: (price?.['TotalPrice'] as number) ?? 0,
            },
            cabin,
            fareBasis,
            classOfService,
            fareType,
            amenities,
          });
        }
      }

      // Deduplicate fare options by brand+price
      const seen = new Set<string>();
      const uniqueFares: FareOption[] = [];
      for (const fo of allFareOptions) {
        const key = `${fo.brandName}|${fo.price.total}|${fo.cabin}`;
        if (!seen.has(key)) {
          seen.add(key);
          uniqueFares.push(fo);
        }
      }

      uniqueFares.sort((a, b) => a.price.total - b.price.total);

      const cheapest = uniqueFares[0]?.price.total ?? 0;
      const currency = uniqueFares[0]?.price.currency ?? '';

      cards.push({
        id,
        sequence,
        origin,
        destination,
        segments: headerSegments,
        fareOptions: uniqueFares,
        cheapestPrice: cheapest,
        currency,
        primaryCarrier,
        totalDuration: formatDuration(totalDuration),
      });
    }

    return cards;
  }

  // --- Formatting helpers ---

  function formatTime(t: string): string {
    if (!t) return '';
    return t.slice(0, 5);
  }

  function formatDuration(d: string): string {
    if (!d) return '';
    const match = d.match(/PT(?:(\d+)H)?(?:(\d+)M)?/);
    if (!match) return d;
    const h = match[1] ? parseInt(match[1]) : 0;
    const m = match[2] ? parseInt(match[2]) : 0;
    if (h === 0) return `${m}m`;
    return `${h}h ${m.toString().padStart(2, '0')}m`;
  }

  function formatPrice(n: number, currency: string): string {
    return `${currency}\u00a0${n.toFixed(2)}`;
  }

  function amenityIcon(inclusion: string): string {
    switch (inclusion) {
      case 'Included': return '\u2713';
      case 'Chargeable': return '$';
      case 'Not Offered': return '\u2717';
      default: return '?';
    }
  }

  function amenityClass(inclusion: string): string {
    switch (inclusion) {
      case 'Included': return 'amenity-included';
      case 'Chargeable': return 'amenity-chargeable';
      case 'Not Offered': return 'amenity-excluded';
      default: return 'amenity-unknown';
    }
  }

  function carrierLogoUrl(carrier: string): string {
    return `https://pics.avs.io/60/30/${carrier}.png`;
  }

  function toggleCard(id: string) {
    const next = new Set(expandedCards);
    if (next.has(id)) {
      next.delete(id);
    } else {
      next.add(id);
    }
    expandedCards = next;
  }

  // --- Computed ---

  let cards = $derived(parse(body));

  let sortedCards = $derived.by((): ItineraryCard[] => {
    const sorted = [...cards];
    switch (sortMode) {
      case 'price':
        sorted.sort((a, b) => a.cheapestPrice - b.cheapestPrice);
        break;
      case 'departure':
        sorted.sort((a, b) => {
          const aTime = a.segments[0]?.departureTime ?? '';
          const bTime = b.segments[0]?.departureTime ?? '';
          return aTime.localeCompare(bTime);
        });
        break;
      case 'duration':
        sorted.sort((a, b) => a.totalDuration.localeCompare(b.totalDuration));
        break;
    }
    return sorted;
  });
</script>

{#if cards.length === 0}
  <div class="viz-empty">
    <p>No itineraries found in response.</p>
  </div>
{:else}
  <div class="viz-toolbar">
    <span class="viz-count">{cards.length} itineraries</span>
    <div class="viz-sort">
      <label for="sort-select">Sort:</label>
      <select id="sort-select" bind:value={sortMode}>
        <option value="price">Cheapest</option>
        <option value="departure">Departure</option>
        <option value="duration">Duration</option>
      </select>
    </div>
  </div>

  <div class="viz-cards">
    {#each sortedCards as card (card.id)}
      {@const isExpanded = expandedCards.has(card.id)}
      {@const cheapest = card.fareOptions[0]}
      <div class="viz-card" class:expanded={isExpanded}>
        <button class="viz-card-header" onclick={() => toggleCard(card.id)}>
          <div class="viz-card-left">
            <img
              class="carrier-logo"
              src={cheapest?.brandImageUrl || carrierLogoUrl(card.primaryCarrier)}
              alt={card.primaryCarrier}
              onerror={(e: Event) => { (e.target as HTMLImageElement).style.display = 'none'; }}
            />
            <div class="viz-route-info">
              {#each card.segments as seg, i}
                <div class="viz-segment" class:viz-segment-connecting={i > 0}>
                  {#if i > 0}
                    <span class="connecting-label">connection</span>
                  {/if}
                  <span class="flight-number">{seg.flightNumber}</span>
                  <span class="route">
                    <span class="station">{seg.origin}</span>
                    <span class="route-arrow">→</span>
                    <span class="station">{seg.destination}</span>
                  </span>
                  <span class="times">
                    {seg.departureTime}
                    <span class="time-arrow">–</span>
                    {seg.arrivalTime}
                  </span>
                  {#if i === 0}
                    <span class="duration">{card.totalDuration || seg.duration}</span>
                  {/if}
                  {#if cheapest && i === 0}
                    <span class="cabin-badge">{cheapest.cabin || 'Economy'}</span>
                  {/if}
                </div>
              {/each}
            </div>
          </div>
          <div class="viz-card-right">
            <span class="viz-price-from">
              <span class="from-label">from</span>
              <span class="price-value">{formatPrice(card.cheapestPrice, card.currency)}</span>
            </span>
            <span class="expand-icon">{isExpanded ? '\u25b2' : '\u25bc'}</span>
          </div>
        </button>

        {#if isExpanded}
          <div class="viz-fares">
            <table class="fare-table">
              <thead>
                <tr>
                  <th>Brand</th>
                  <th>Cabin</th>
                  <th>Class</th>
                  <th>Fare Basis</th>
                  <th class="col-amenities">Amenities</th>
                  <th class="col-price">Base</th>
                  <th class="col-price">Taxes</th>
                  <th class="col-price">Total</th>
                </tr>
              </thead>
              <tbody>
                {#each card.fareOptions as fare}
                  <tr>
                    <td class="cell-brand">
                      {#if fare.brandImageUrl}
                        <img
                          class="brand-logo-sm"
                          src={fare.brandImageUrl}
                          alt=""
                          onerror={(e: Event) => { (e.target as HTMLImageElement).style.display = 'none'; }}
                        />
                      {/if}
                      <span>{fare.brandName}</span>
                    </td>
                    <td>{fare.cabin || '\u2014'}</td>
                    <td><code>{fare.classOfService || '\u2014'}</code></td>
                    <td><code>{fare.fareBasis || '\u2014'}</code></td>
                    <td class="cell-amenities">
                      {#each fare.amenities as am}
                        <span
                          class="amenity-pill {amenityClass(am.inclusion)}"
                          title="{am.name}: {am.inclusion}"
                        >{amenityIcon(am.inclusion)} {am.name}</span>
                      {/each}
                    </td>
                    <td class="col-price">{formatPrice(fare.price.base, fare.price.currency)}</td>
                    <td class="col-price">{formatPrice(fare.price.taxes, fare.price.currency)}</td>
                    <td class="col-price price-total">{formatPrice(fare.price.total, fare.price.currency)}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </div>
    {/each}
  </div>
{/if}

<style>
  .viz-empty {
    padding: 2rem;
    text-align: center;
    color: var(--color-text-muted);
  }

  .viz-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 1rem;
    padding: 0.5rem 0;
  }

  .viz-count {
    font-size: 0.85rem;
    color: var(--color-text-muted);
  }

  .viz-sort {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.85rem;
    color: var(--color-text-muted);
  }

  .viz-sort select {
    font-family: var(--font-sans);
    font-size: 0.85rem;
    padding: 0.25rem 0.5rem;
    background: var(--color-surface);
    color: var(--color-text);
    border: 1px solid var(--color-border);
    border-radius: 4px;
    cursor: pointer;
  }

  .viz-cards {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .viz-card {
    border: 1px solid var(--color-border);
    border-radius: 8px;
    overflow: hidden;
    background: var(--color-surface);
    transition: border-color 0.15s;
  }

  .viz-card:hover {
    border-color: var(--color-primary);
  }

  .viz-card.expanded {
    border-color: var(--color-primary);
  }

  .viz-card-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    padding: 0.75rem 1rem;
    background: transparent;
    border: none;
    cursor: pointer;
    color: var(--color-text);
    text-align: left;
    gap: 1rem;
  }

  .viz-card-header:hover {
    background: rgba(99, 102, 241, 0.04);
  }

  .viz-card-left {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    flex: 1;
    min-width: 0;
  }

  .carrier-logo {
    width: 40px;
    height: 28px;
    object-fit: contain;
    border-radius: 3px;
    flex-shrink: 0;
  }

  .viz-route-info {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
  }

  .viz-segment {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    font-size: 0.85rem;
    flex-wrap: wrap;
  }

  .viz-segment-connecting {
    padding-left: 0.5rem;
    border-left: 2px solid var(--color-border);
  }

  .connecting-label {
    font-size: 0.7rem;
    color: var(--color-text-muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .flight-number {
    font-weight: 600;
    font-family: var(--font-mono);
    font-size: 0.85rem;
    min-width: 4.5em;
  }

  .route {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    font-weight: 600;
  }

  .station {
    font-family: var(--font-mono);
  }

  .route-arrow {
    color: var(--color-text-muted);
    font-size: 0.8rem;
  }

  .times {
    color: var(--color-text-muted);
    font-family: var(--font-mono);
    font-size: 0.8rem;
  }

  .time-arrow {
    margin: 0 0.15rem;
  }

  .duration {
    font-size: 0.75rem;
    color: var(--color-text-muted);
    background: rgba(99, 102, 241, 0.08);
    padding: 0.1rem 0.4rem;
    border-radius: 3px;
  }

  .cabin-badge {
    font-size: 0.7rem;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    color: var(--color-primary);
    background: rgba(99, 102, 241, 0.1);
    padding: 0.1rem 0.4rem;
    border-radius: 3px;
    font-weight: 600;
  }

  .viz-card-right {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    flex-shrink: 0;
  }

  .viz-price-from {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
  }

  .from-label {
    font-size: 0.65rem;
    color: var(--color-text-muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .price-value {
    font-size: 1rem;
    font-weight: 700;
    color: var(--color-success);
    font-family: var(--font-mono);
  }

  .expand-icon {
    font-size: 0.7rem;
    color: var(--color-text-muted);
  }

  /* --- Fare table --- */

  .viz-fares {
    border-top: 1px solid var(--color-border);
    overflow-x: auto;
  }

  .fare-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.8rem;
  }

  .fare-table thead {
    background: rgba(0, 0, 0, 0.2);
  }

  .fare-table th {
    padding: 0.4rem 0.6rem;
    text-align: left;
    font-weight: 600;
    color: var(--color-text-muted);
    font-size: 0.7rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    white-space: nowrap;
  }

  .fare-table td {
    padding: 0.5rem 0.6rem;
    border-top: 1px solid var(--color-border);
    white-space: nowrap;
  }

  .fare-table tbody tr:hover {
    background: rgba(99, 102, 241, 0.04);
  }

  .cell-brand {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    font-weight: 500;
  }

  .brand-logo-sm {
    width: 24px;
    height: 16px;
    object-fit: contain;
    border-radius: 2px;
  }

  .col-price {
    text-align: right;
    font-family: var(--font-mono);
    font-size: 0.8rem;
  }

  .price-total {
    font-weight: 700;
    color: var(--color-success);
  }

  .col-amenities {
    min-width: 10rem;
  }

  .cell-amenities {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
  }

  .amenity-pill {
    font-size: 0.65rem;
    padding: 0.1rem 0.35rem;
    border-radius: 3px;
    white-space: nowrap;
  }

  .amenity-included {
    color: var(--color-success);
    background: rgba(34, 197, 94, 0.1);
  }

  .amenity-chargeable {
    color: var(--color-warning);
    background: rgba(245, 158, 11, 0.1);
  }

  .amenity-excluded {
    color: var(--color-text-muted);
    background: rgba(156, 163, 175, 0.08);
  }

  .amenity-unknown {
    color: var(--color-text-muted);
    background: rgba(156, 163, 175, 0.08);
  }

  code {
    background: transparent;
    padding: 0;
  }
</style>
