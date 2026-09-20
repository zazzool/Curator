// Слова, которые студия согласует с числами.
//
// Одно место на всю студию намеренно. Правило это списывают по месту, и
// списанное расходится молча: в приложении мы уже написали «рубрикы»,
// взяв окончание по последней букве слова, а не по основе.

/** «1 задача», «2 задачи», «11 задач». */
export function счётом(n: number, one: string, few: string, many: string): string {
  const last = n % 10
  const tens = n % 100
  if (tens >= 11 && tens <= 14) return `${n} ${many}`
  if (last === 1) return `${n} ${one}`
  if (last >= 2 && last <= 4) return `${n} ${few}`
  return `${n} ${many}`
}

/** Время сервера — днём, каким его назовёт человек. */
export function датой(raw: string): string {
  const at = new Date(raw)
  if (Number.isNaN(at.getTime())) return raw
  return at.toLocaleDateString('ru-RU', { day: 'numeric', month: 'long', year: 'numeric' })
}

/**
 * Дата и время — для того, что случилось только что.
 *
 * Одной даты хватает событию недельной давности, но не черновику,
 * потерянному пять минут назад: «от 20 сентября» не говорит человеку,
 * то ли это, что он набирал перед тем, как вкладка исчезла.
 */
export function датойИвременем(raw: string): string {
  const at = new Date(raw)
  if (Number.isNaN(at.getTime())) return raw
  return at.toLocaleString('ru-RU', {
    day: 'numeric',
    month: 'long',
    hour: '2-digit',
    minute: '2-digit',
  })
}
