import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'

import { NotFound } from './NotFound'

describe('ненайденный адрес', () => {
  beforeEach(() => {
    window.history.replaceState(null, '', '/истопники')
  })

  it('называет сам адрес', () => {
    // Чаще всего адрес и есть ответ: ошибку видно глазом — лишний знак,
    // приехавший из письма, съеденная косая черта.
    render(<NotFound path="/истопники" />)
    expect(screen.getByText('/истопники')).toBeTruthy()
  })

  it('не выдаёт себя за список источников', () => {
    // Подстановка раздела по умолчанию — это ложь: человек решит, что
    // попал куда хотел, и будет искать пропавшее там, где не был.
    render(<NotFound path="/истопники" />)
    expect(screen.getByRole('heading', { level: 2 }).textContent).toContain('нет')
  })

  it('даёт выход с себя самой', () => {
    // Попавший сюда пришёл работать, и отправлять его глазами в колонку
    // разделов незачем.
    render(<NotFound path="/истопники" />)
    const выход = screen.getByRole('button', { name: 'К задачам' })
    выход.click()
    expect(window.location.pathname).toBe('/cases')
  })
})
