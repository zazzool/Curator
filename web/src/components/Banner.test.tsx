import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { Banner } from './Banner'

describe('полоса отказа и успеха', () => {
  it('отказ объявляется как alert', () => {
    // Составитель с экранным диктором жал «Раздавать», публикация
    // отклонялась с четырьмя замечаниями, и не говорилось НИЧЕГО: фокус
    // оставался на кнопке, а с его стороны нажатие не сделало ничего. И
    // он жал снова.
    render(<Banner kind="error">Выверьте указания к этому коду</Banner>)
    expect(screen.getByRole('alert').textContent).toBe('Выверьте указания к этому коду')
  })

  it('успех объявляется как status, а не перебивает диктора', () => {
    // alert перебивает на полуслове: верно для остановившего работу и
    // неверно для «Карточка сохранена». Перебитый на каждое удачное
    // действие человек выключает диктор — и не услышит уже ничего.
    render(<Banner kind="success">Карточка сохранена</Banner>)
    expect(screen.getByRole('status').textContent).toBe('Карточка сохранена')
    expect(screen.queryByRole('alert')).toBeNull()
  })
})
