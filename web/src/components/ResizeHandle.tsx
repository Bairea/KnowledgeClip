import { useCallback, useEffect, useRef, useState } from 'react'

interface ResizeHandleProps {
  direction: 'horizontal' | 'vertical'
  onResize: (delta: number) => void
  min: number
  max: number
  current: number
}

export default function ResizeHandle({ direction, onResize, min, max, current }: ResizeHandleProps) {
  const [isDragging, setIsDragging] = useState(false)
  const lastPos = useRef(0)

  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    lastPos.current = direction === 'horizontal' ? e.clientX : e.clientY
    setIsDragging(true)
  }, [direction])

  useEffect(() => {
    if (!isDragging) return

    const handleMouseMove = (e: MouseEvent) => {
      const currentPos = direction === 'horizontal' ? e.clientX : e.clientY
      const delta = currentPos - lastPos.current
      lastPos.current = currentPos
      onResize(delta)
    }

    const handleMouseUp = () => {
      setIsDragging(false)
    }

    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)
    document.body.style.cursor = direction === 'horizontal' ? 'col-resize' : 'row-resize'
    document.body.style.userSelect = 'none'

    return () => {
      document.removeEventListener('mousemove', handleMouseMove)
      document.removeEventListener('mouseup', handleMouseUp)
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
    }
  }, [isDragging, direction, onResize])

  const isHorizontal = direction === 'horizontal'
  const isAtMin = current <= min
  const isAtMax = current >= max

  return (
    <div
      data-resize-handle={direction}
      onMouseDown={handleMouseDown}
      className={`group relative z-10 flex shrink-0 items-center justify-center bg-[var(--line-soft)] transition-colors hover:bg-[var(--line)] ${
        isHorizontal ? 'w-1 cursor-col-resize' : 'h-1 cursor-row-resize'
      } ${isDragging ? 'bg-[var(--line)]' : ''}`}
    >
      <div
        className={`bg-[var(--ink-muted)] transition-colors group-hover:bg-[var(--ink-soft)] ${
          isHorizontal
            ? 'h-10 w-px'
            : 'h-px w-10'
        } ${isDragging ? 'bg-[var(--accent)]' : ''} ${isAtMin || isAtMax ? 'opacity-30' : 'opacity-50'}`}
      />
    </div>
  )
}
