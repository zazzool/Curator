import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { Toolbar } from './Toolbar'

describe('верхняя полоса', () => {
  it('называет, где мы, заголовком первого уровня', () => {
    // Заголовка первого уровня в студии не было ни одного, а читающий с
    // экрана начинает работу с него: «где я» он спрашивает у заголовков,
    // а не у цвета полосы.
    render(<Toolbar section="sources" crumb="Приказ №1183н" onSignOut={() => {}} />)
    const где = screen.getByRole('heading', { level: 1 })
    expect(где.textContent).toContain('Источники')
    expect(где.textContent).toContain('Приказ №1183н')
  })

  it('первый уровень ровно один', () => {
    // Два первых уровня на странице — то же самое, что ни одного:
    // читающий с экрана переходит по ним и не понимает, который из них
    // называет место.
    render(<Toolbar section="packs" onSignOut={() => {}} />)
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
  })
})
