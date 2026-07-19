import {
  ComposerPrimitive,
  type Unstable_IconComponent,
  type Unstable_TriggerItem,
  unstable_defaultDirectiveFormatter,
} from "@assistant-ui/react"
import { ChevronLeftIcon, ChevronRightIcon, SparklesIcon } from "lucide-react"
import { memo, type ComponentPropsWithoutRef, type FC } from "react"

import { cn } from "@/lib/utils"

type ComposerTriggerPopoverProps = Omit<
  ComponentPropsWithoutRef<typeof ComposerPrimitive.Unstable_TriggerPopover>,
  "children"
> & {
  iconMap?: Record<string, Unstable_IconComponent>
  fallbackIcon?: Unstable_IconComponent
  directive?: {
    onInserted?: (item: Unstable_TriggerItem) => void
  }
  action?: {
    onExecute: (item: Unstable_TriggerItem) => void
    removeOnExecute?: boolean
  }
}

function resolveIcon(
  iconKey: string | undefined,
  iconMap: Record<string, Unstable_IconComponent> | undefined,
  fallbackIcon: Unstable_IconComponent,
) {
  if (iconKey && iconMap?.[iconKey]) {
    return iconMap[iconKey]
  }
  return fallbackIcon
}

const ComposerTriggerPopoverImpl: FC<ComposerTriggerPopoverProps> = ({
  className,
  iconMap,
  fallbackIcon = SparklesIcon,
  directive,
  action,
  ...props
}) => {
  return (
    <ComposerPrimitive.Unstable_TriggerPopover
      className={cn(
        "absolute bottom-full left-0 z-50 mb-2 w-[min(22rem,calc(100vw-2rem))] overflow-hidden rounded-lg border bg-popover text-popover-foreground shadow-lg",
        className,
      )}
      {...props}
    >
      {directive ? (
        <ComposerPrimitive.Unstable_TriggerPopover.Directive
          formatter={unstable_defaultDirectiveFormatter}
          onInserted={directive.onInserted}
        />
      ) : null}
      {action ? (
        <ComposerPrimitive.Unstable_TriggerPopover.Action
          formatter={unstable_defaultDirectiveFormatter}
          onExecute={action.onExecute}
          removeOnExecute={action.removeOnExecute}
        />
      ) : null}
      <ComposerPrimitive.Unstable_TriggerPopoverCategories>
        {(categories) => (
          <div className="max-h-72 overflow-y-auto py-1">
            {categories.map((category) => {
              const Icon = resolveIcon(category.id, iconMap, fallbackIcon)
              return (
                <ComposerPrimitive.Unstable_TriggerPopoverCategoryItem
                  key={category.id}
                  categoryId={category.id}
                  className="flex w-full cursor-pointer items-center justify-between gap-3 px-3 py-2 text-left text-sm outline-none transition-colors hover:bg-accent data-[highlighted]:bg-accent"
                >
                  <span className="flex min-w-0 items-center gap-2">
                    <Icon className="size-4 shrink-0 text-muted-foreground" />
                    <span className="truncate">{category.label}</span>
                  </span>
                  <ChevronRightIcon className="size-4 shrink-0 text-muted-foreground" />
                </ComposerPrimitive.Unstable_TriggerPopoverCategoryItem>
              )
            })}
          </div>
        )}
      </ComposerPrimitive.Unstable_TriggerPopoverCategories>
      <ComposerPrimitive.Unstable_TriggerPopoverItems>
        {(items) => (
          <div>
            <ComposerPrimitive.Unstable_TriggerPopoverBack className="flex w-full cursor-pointer items-center gap-1.5 border-b px-3 py-2 text-xs font-medium text-muted-foreground outline-none transition-colors hover:bg-accent">
              <ChevronLeftIcon className="size-3.5" />
              Back
            </ComposerPrimitive.Unstable_TriggerPopoverBack>
            <div className="max-h-72 overflow-y-auto py-1">
              {items.map((item, index) => {
                const iconKey = typeof item.metadata?.icon === "string" ? item.metadata.icon : undefined
                const Icon = resolveIcon(iconKey, iconMap, fallbackIcon)
                return (
                  <ComposerPrimitive.Unstable_TriggerPopoverItem
                    key={item.id}
                    item={item}
                    index={index}
                    className="flex w-full cursor-pointer flex-col items-start gap-1 px-3 py-2 text-left outline-none transition-colors hover:bg-accent data-[highlighted]:bg-accent"
                  >
                    <span className="flex min-w-0 items-center gap-2 text-sm font-medium">
                      <Icon className="size-3.5 shrink-0 text-primary" />
                      <span className="truncate">{item.label}</span>
                    </span>
                    {item.description ? (
                      <span className="line-clamp-2 pl-5 text-xs leading-5 text-muted-foreground">
                        {item.description}
                      </span>
                    ) : null}
                  </ComposerPrimitive.Unstable_TriggerPopoverItem>
                )
              })}
            </div>
          </div>
        )}
      </ComposerPrimitive.Unstable_TriggerPopoverItems>
    </ComposerPrimitive.Unstable_TriggerPopover>
  )
}

export const ComposerTriggerPopover = memo(ComposerTriggerPopoverImpl)
