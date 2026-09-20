import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ErrorBoundary } from './ErrorBoundary'

function Падающий(): never {
  // Ровно то, что делает задача без `options`: перебор отсутствующего во
  // время отрисовки.
  const тело = null as unknown as { options: string[] }
  throw new TypeError(`нет поля: ${тело.options.length}`)
}

describe('граница отказа', () => {
  beforeEach(() => {
    // React печатает пойманный отказ сам, и граница печатает его же.
    // В выводе проверок это выглядит как поломка набора.
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('кривая задача стоит панели, а не всей студии', async () => {
    // React снимает ВСЁ дерево, когда отрисовка бросает. Без границы
    // белый экран получался не в той панели, где беда, а во всей студии:
    // пропадали и колонка разделов, и имя вошедшего, и выход на другие
    // экраны.
    render(
      <div>
        <p>колонка разделов</p>
        <ErrorBoundary>
          <Падающий />
        </ErrorBoundary>
      </div>,
    )
    expect(await screen.findByText(/не отрисовался/)).toBeTruthy()
    expect(screen.getByText('колонка разделов')).toBeTruthy()
  })

  it('исправное показывается как есть', () => {
    render(
      <ErrorBoundary>
        <p>задача целая</p>
      </ErrorBoundary>,
    )
    expect(screen.getByText('задача целая')).toBeTruthy()
    expect(screen.queryByText(/не отрисовался/)).toBeNull()
  })

  it('переход на другой экран снимает упавшее', () => {
    // Сброс сделан ключом снаружи, а не способом внутри. Останься
    // упавшее состояние — отказ на одном экране висел бы и на исправных,
    // и составитель решил бы, что сломалась студия целиком.
    const { rerender } = render(
      <ErrorBoundary key="sources">
        <Падающий />
      </ErrorBoundary>,
    )
    expect(screen.getByText(/не отрисовался/)).toBeTruthy()

    rerender(
      <ErrorBoundary key="packs">
        <p>наборы</p>
      </ErrorBoundary>,
    )
    expect(screen.getByText('наборы')).toBeTruthy()
    expect(screen.queryByText(/не отрисовался/)).toBeNull()
  })
})
