import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  arrayMove,
  rectSortingStrategy,
  sortableKeyboardCoordinates,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useReorderPanels } from "@/hooks/use-dashboards";
import type { BaselineParams } from "@/hooks/use-series";
import type { RangeTokens } from "@/lib/time-range";
import type { Metric, Panel } from "@/lib/types";
import { cn } from "@/lib/utils";
import { DragHandle, PanelCard } from "./panel-card";

// A Panel's width preset maps to a column span. The grid itself is an auto-fit of
// 320px-minimum tracks rather than a fixed three columns, so the number of columns
// follows the space available and not a breakpoint guess: a wide monitor gets four
// or five, a split window gets two, and nothing is ever squeezed under its minimum.
// A span is then capped by the track count in CSS, which is what `min()` on the
// span cannot express — hence the breakpoint qualifiers, which only ever *reduce*
// a span on a narrow grid.
const WIDTH_CLASS: Record<number, string> = {
  1: "col-span-1",
  2: "col-span-1 md:col-span-2",
  3: "col-span-1 md:col-span-2 xl:col-span-3",
};

/** DashboardGrid lays the Panels out in a responsive 3-column grid and makes
 *  them drag-reorderable via dnd-kit (ADR 0013). A drop persists the new order
 *  through the reorder mutation, which updates the cache optimistically. */
export function DashboardGrid({
  dashboardId,
  panels,
  metrics,
  range,
  baseline,
  showAnnotations,
}: {
  dashboardId: number;
  panels: Panel[];
  metrics: Map<string, Metric>;
  range: RangeTokens;
  baseline?: BaselineParams;
  showAnnotations: boolean;
}) {
  const reorder = useReorderPanels();
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const onDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const oldIndex = panels.findIndex((p) => p.id === active.id);
    const newIndex = panels.findIndex((p) => p.id === over.id);
    if (oldIndex < 0 || newIndex < 0) return;
    const panelIds = arrayMove(panels, oldIndex, newIndex).map((p) => p.id);
    reorder.mutate({ dashboardId, panelIds });
  };

  return (
    <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
      <SortableContext items={panels.map((p) => p.id)} strategy={rectSortingStrategy}>
        <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(20rem,1fr))]">
          {panels.map((panel) => (
            <SortablePanel
              key={panel.id}
              panel={panel}
              catalog={metrics}
              range={range}
              baseline={baseline}
              showAnnotations={showAnnotations}
            />
          ))}
        </div>
      </SortableContext>
    </DndContext>
  );
}

function SortablePanel({
  panel,
  catalog,
  range,
  baseline,
  showAnnotations,
}: {
  panel: Panel;
  catalog: Map<string, Metric>;
  range: RangeTokens;
  baseline?: BaselineParams;
  showAnnotations: boolean;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: panel.id });
  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    zIndex: isDragging ? 10 : undefined,
    opacity: isDragging ? 0.85 : 1,
  };

  return (
    <div ref={setNodeRef} style={style} className={cn(WIDTH_CLASS[panel.width] ?? WIDTH_CLASS[1])} {...attributes}>
      <PanelCard
        panel={panel}
        catalog={catalog}
        range={range}
        baseline={baseline}
        showAnnotations={showAnnotations}
        dragHandle={<DragHandle {...listeners} />}
      />
    </div>
  );
}
