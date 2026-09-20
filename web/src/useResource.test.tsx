import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useCallback } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { ApiError } from './api'
import { Loaded, useResource } from './useResource'

// Экран в одну строку: он и есть предмет проверки. Настоящие разделы
// студии добавили бы к нему свои таблицы и свои отборы, и отказ читался бы
// как отказ таблицы.
function Экран({ read }: { read: () => Promise<string[]> }) {
  const stable = useCallback(read, [read])
  const list = useResource(stable, 'Не прочитано')
  return (
    <Loaded from={list} while="Читаем список…">
      {(items) => (
        <ul>
          {items.map((item) => (
            <li key={item}>{item}</li>
          ))}
        </ul>
      )}
    </Loaded>
  )
}

describe('чтение с сервера', () => {
  it('показывает прочитанное', async () => {
    render(<Экран read={async () => ['первый', 'второй']} />)
    expect(await screen.findByText('первый')).toBeTruthy()
    expect(screen.queryByText('Читаем список…')).toBeNull()
  })

  it('на отказе показывает отказ и НЕ показывает «читаем»', async () => {
    // Ровно та поломка, ради которой крючок и заведён: составитель без
    // права открывал раздел, получал внятный отказ по-русски и под ним
    // вечное «Читаем…» — студия говорила ему, что грузится то, чего не
    // будет никогда.
    render(
      <Экран
        read={async () => {
          throw new ApiError(403, 'Этот раздел вам не открыт', [])
        }}
      />,
    )
    expect(await screen.findByText('Этот раздел вам не открыт')).toBeTruthy()
    expect(screen.queryByText('Читаем список…')).toBeNull()
  })

  it('отказ без слов заменяется своими словами', async () => {
    render(
      <Экран
        read={async () => {
          throw new Error('TypeError: Failed to fetch')
        }}
      />,
    )
    // Врачу и составителю показывается русский текст, а не то, чем
    // отказала сеть: второе он прочтёт как поломку студии.
    expect(await screen.findByText('Не прочитано')).toBeTruthy()
    expect(screen.queryByText(/Failed to fetch/)).toBeNull()
  })

  it('ответ обогнавшего чтения не затирает свежий', async () => {
    // Два чтения в полёте — обычное дело: сменили отбор, пока шло первое.
    // Ответ ПЕРВОГО приходит вторым чаще, чем кажется, и без счётчика
    // походов он затирает свежий — молча и не воспроизводимо.
    const отпустить: Array<(value: string[]) => void> = []
    const read = () =>
      new Promise<string[]>((resolve) => {
        отпустить.push(resolve)
      })

    function Дважды() {
      const stable = useCallback(read, [])
      const list = useResource(stable, 'Не прочитано')
      return (
        <>
          <button onClick={() => void list.reload()}>Ещё раз</button>
          <Loaded from={list}>{(items) => <p>{items.join(', ')}</p>}</Loaded>
        </>
      )
    }

    render(<Дважды />)
    await waitFor(() => expect(отпустить.length).toBe(1))
    fireEvent.click(screen.getByText('Ещё раз'))
    await waitFor(() => expect(отпустить.length).toBe(2))

    // Отпускаем ВТОРОЕ, потом первое — то есть старый ответ приходит
    // последним.
    const [первый, второй] = отпустить
    второй?.(['свежее'])
    expect(await screen.findByText('свежее')).toBeTruthy()
    первый?.(['устаревшее'])
    await new Promise((done) => setTimeout(done, 0))
    expect(screen.queryByText('устаревшее')).toBeNull()
    expect(screen.getByText('свежее')).toBeTruthy()
  })

  it('перечитывание не гасит уже показанное', async () => {
    // Иначе список пропадает на время каждого обновления, и страница
    // прыгает после всякого действия составителя.
    let ответ = ['первый']
    const read = async () => ответ

    function Снова() {
      const stable = useCallback(read, [])
      const list = useResource(stable, 'Не прочитано')
      return (
        <>
          <button onClick={() => void list.reload()}>Ещё раз</button>
          <Loaded from={list} while="Читаем список…">
            {(items) => <p>{items.join(', ')}</p>}
          </Loaded>
        </>
      )
    }

    render(<Снова />)
    expect(await screen.findByText('первый')).toBeTruthy()
    ответ = ['второй']
    fireEvent.click(screen.getByText('Ещё раз'))
    expect(screen.queryByText('Читаем список…')).toBeNull()
    expect(await screen.findByText('второй')).toBeTruthy()
  })

  it('подменённое значение переживает чтение, шедшее рядом', async () => {
    // Действие уже вернуло новое значение, и перечитывать его вторым
    // запросом значит показать составителю старое между двумя ответами.
    const отпустить: Array<(value: string[]) => void> = []
    const read = () =>
      new Promise<string[]>((resolve) => {
        отпустить.push(resolve)
      })

    function Подмена() {
      const stable = useCallback(read, [])
      const list = useResource(stable, 'Не прочитано')
      return (
        <>
          <button onClick={() => list.set(['подменённое'])}>Подменить</button>
          <Loaded from={list}>{(items) => <p>{items.join(', ')}</p>}</Loaded>
        </>
      )
    }

    render(<Подмена />)
    await waitFor(() => expect(отпустить.length).toBe(1))
    fireEvent.click(screen.getByText('Подменить'))
    expect(await screen.findByText('подменённое')).toBeTruthy()

    отпустить[0]?.(['прочитанное'])
    await new Promise((done) => setTimeout(done, 0))
    expect(screen.queryByText('прочитанное')).toBeNull()
  })
})

describe('чтение без крючка', () => {
  it('не заводится второй раз тем же чтением', async () => {
    // Чтение, меняющееся на каждой отрисовке, уводит экран в вечный круг
    // запросов. Признак у этого отказа никакой: страница просто «долго
    // думает», а сервер получает сотню запросов в секунду.
    const read = vi.fn(async () => ['один'])
    render(<Экран read={read} />)
    expect(await screen.findByText('один')).toBeTruthy()
    await new Promise((done) => setTimeout(done, 20))
    expect(read).toHaveBeenCalledTimes(1)
  })
})
