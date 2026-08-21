import * as React from "react";
import L from "leaflet";
import "leaflet/dist/leaflet.css";
import { useMapConfig } from "@/hooks/use-auth";
import type { RouteGeometry } from "@/lib/types";

/** RouteMap draws a workout's Routes.
 *
 *  The basemap is opt-in and off by default (ADR 0028). With no `VERVE_MAP_TILES`
 *  configured the server sends no map config, no tile layer is added, and the
 *  browser makes no outbound request at all: the trace is drawn on the page's own
 *  ground. That is what keeps "Verve does not phone home" literally true, since a
 *  default basemap would tell a third party where its owner runs. When a tile URL
 *  is configured its attribution is rendered with it, which is not optional.
 *
 *  Each Route is its own polyline. They are never joined: the segment between the
 *  end of one and the start of the next is ground that was never covered. */
function RouteMap({ routes, className }: { routes: RouteGeometry[]; className?: string }) {
  const container = React.useRef<HTMLDivElement>(null);
  const basemap = useMapConfig();
  const tiles = basemap.data?.tiles;
  const attribution = basemap.data?.attribution;

  React.useEffect(() => {
    const el = container.current;
    if (!el) return;
    const lines = routes.map((r) => r.points).filter((pts) => pts.length > 0);
    if (lines.length === 0) return;

    const map = L.map(el, { attributionControl: Boolean(tiles), zoomControl: true });
    if (tiles) {
      L.tileLayer(tiles, { attribution: attribution ?? "" }).addTo(map);
    }

    const colors = getComputedStyle(document.documentElement);
    const stroke = colors.getPropertyValue("--chart-1").trim();
    const layers = lines.map((pts) =>
      L.polyline(pts, {
        // The fallback is the same token by another route: every Palette defines
        // --chart-1 (enforced by the palette contract), so the computed read only
        // fails before the stylesheet has applied — and a literal here would be the
        // one colour on the page that ignores the Palette.
        color: stroke ? `hsl(${stroke})` : "hsl(var(--chart-1))",
        weight: 3,
        opacity: 0.9,
      }).addTo(map),
    );
    map.fitBounds(L.featureGroup(layers).getBounds(), { padding: [16, 16] });

    return () => {
      map.remove();
    };
  }, [routes, tiles, attribution]);

  if (routes.every((r) => r.points.length === 0)) return null;

  return <div ref={container} className={className} />;
}
// Default export so the detail page can lazy-load it: leaflet and its stylesheet
// are ~150 kB that only a workout with a trace ever needs, and the rest of Verve
// should not carry them on first paint.
export default RouteMap;

