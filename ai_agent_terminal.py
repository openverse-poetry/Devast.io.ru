#!/usr/bin/env python3
"""
AI Terminal Agent с использованием LangChain и Ollama.
Агент может выполнять команды в терминале, читать/записывать файлы и исправлять ошибки в коде.
"""

import os
import subprocess
import sys
from typing import Type
from langchain.agents import AgentExecutor, create_react_agent
from langchain_community.llms import Ollama
from langchain_core.prompts import PromptTemplate
from langchain_core.tools import BaseTool
from pydantic import BaseModel, Field


# --- Инструменты (Tools) ---

class ShellInput(BaseModel):
    command: str = Field(description="Команда для выполнения в терминале")


class ShellTool(BaseTool):
    name = "shell"
    description = "Выполняет команду в системной оболочке (CMD/PowerShell/Bash) и возвращает вывод. Используйте для запуска скриптов, компиляции кода, навигации по файлам."
    args_schema: Type[BaseModel] = ShellInput

    def _run(self, command: str) -> str:
        try:
            # Определяем оболочку в зависимости от ОС
            if sys.platform == "win32":
                shell_cmd = ["powershell", "-Command", command]
            else:
                shell_cmd = ["/bin/bash", "-c", command]
            
            result = subprocess.run(
                shell_cmd,
                capture_output=True,
                text=True,
                timeout=60,  # Таймаут 60 секунд
                cwd="/workspace"  # Рабочая директория
            )
            
            output = ""
            if result.stdout:
                output += f"STDOUT:\n{result.stdout}\n"
            if result.stderr:
                output += f"STDERR:\n{result.stderr}\n"
            if not output:
                output = "Команда выполнена успешно (нет вывода)."
            
            output += f"\nКод возврата: {result.returncode}"
            return output
        except subprocess.TimeoutExpired:
            return "Ошибка: Время выполнения команды истекло (лимит 60 сек)."
        except Exception as e:
            return f"Ошибка выполнения команды: {str(e)}"


class ReadFileInput(BaseModel):
    filepath: str = Field(description="Путь к файлу относительно /workspace")


class ReadFileTool(BaseTool):
    name = "read_file"
    description = "Читает содержимое файла. Используйте для анализа кода, логов или конфигураций."
    args_schema: Type[BaseModel] = ReadFileInput

    def _run(self, filepath: str) -> str:
        full_path = os.path.join("/workspace", filepath)
        if not os.path.exists(full_path):
            return f"Ошибка: Файл не найден: {full_path}"
        if not os.path.isfile(full_path):
            return f"Ошибка: Путь не является файлом: {full_path}"
        
        try:
            with open(full_path, 'r', encoding='utf-8') as f:
                content = f.read()
            return f"Содержимое файла {filepath}:\n{content}"
        except Exception as e:
            return f"Ошибка чтения файла: {str(e)}"


class WriteFileInput(BaseModel):
    filepath: str = Field(description="Путь к файлу относительно /workspace")
    content: str = Field(description="Содержимое для записи в файл")


class WriteFileTool(BaseTool):
    name = "write_file"
    description = "Записывает содержимое в файл (перезаписывает существующий или создает новый). Используйте для исправления кода или создания скриптов."
    args_schema: Type[BaseModel] = WriteFileInput

    def _run(self, filepath: str, content: str) -> str:
        full_path = os.path.join("/workspace", filepath)
        try:
            # Создаем директорию, если она не существует
            dir_name = os.path.dirname(full_path)
            if dir_name:
                os.makedirs(dir_name, exist_ok=True)
            
            with open(full_path, 'w', encoding='utf-8') as f:
                f.write(content)
            return f"Файл успешно записан: {filepath}"
        except Exception as e:
            return f"Ошибка записи файла: {str(e)}"


class ListDirInput(BaseModel):
    path: str = Field(default=".", description="Путь к директории относительно /workspace")


class ListDirTool(BaseTool):
    name = "list_dir"
    description = "Выводит список файлов и папок в указанной директории."
    args_schema: Type[BaseModel] = ListDirInput

    def _run(self, path: str = ".") -> str:
        full_path = os.path.join("/workspace", path)
        if not os.path.exists(full_path):
            return f"Ошибка: Директория не найдена: {full_path}"
        
        try:
            items = os.listdir(full_path)
            formatted_list = "\n".join(items)
            return f"Содержимое директории {path}:\n{formatted_list}"
        except Exception as e:
            return f"Ошибка чтения директории: {str(e)}"


# --- Настройка агента ---

def create_agent():
    # Инициализация LLM через Ollama
    llm = Ollama(
        model="llama3.2",  # Убедитесь, что модель скачана: ollama pull llama3.2
        base_url="http://localhost:11434",
        temperature=0.1  # Низкая температура для более точного выполнения команд
    )

    # Набор инструментов
    tools = [
        ShellTool(),
        ReadFileTool(),
        WriteFileTool(),
        ListDirTool()
    ]

    # Промпт для агента
    prompt_template = """Ты — автономный AI-агент для работы в терминале. Твоя задача — выполнять приказы пользователя, используя доступные инструменты.
    
Доступные инструменты:
1. `shell` - выполнение команд в терминале (компиляция, запуск скриптов, навигация).
2. `read_file` - чтение файлов (анализ кода, логов).
3. `write_file` - запись файлов (исправление ошибок, создание скриптов).
4. `list_dir` - просмотр содержимого директорий.

Правила:
- Всегда работай в директории `/workspace`.
- Если пользователь просит исправить ошибку в `main.go`, сначала прочитай файл, найди ошибку, затем запиши исправленную версию.
- После выполнения команды через `shell` докладывай о результате (успех/ошибка, вывод команды).
- Действуй пошагово: план → выполнение → проверка результата.
- Если команда завершается ошибкой, проанализируй STDERR и предложи решение.

Начинай работу сразу после получения приказа. Не задавай лишних вопросов, действуй самостоятельно.

Приказ пользователя: {input}
{agent_scratchpad}
"""

    prompt = PromptTemplate.from_template(prompt_template)

    # Создание агента
    agent = create_react_agent(llm, tools, prompt)
    agent_executor = AgentExecutor(
        agent=agent,
        tools=tools,
        verbose=True,
        handle_parsing_errors=True,
        max_iterations=10  # Максимум шагов для выполнения задачи
    )

    return agent_executor


# --- Основной цикл ---

def main():
    print("🤖 AI Terminal Agent запущен!")
    print("Рабочая директория: /workspace")
    print("Доступные команды: вводите приказы на естественном языке.")
    print("Для выхода введите 'exit' или 'quit'.\n")

    try:
        agent = create_agent()
    except Exception as e:
        print(f"❌ Ошибка инициализации агента: {e}")
        print("Убедитесь, что Ollama запущен (`ollama serve`) и модель `llama3.2` установлена (`ollama pull llama3.2`).")
        sys.exit(1)

    while True:
        try:
            user_input = input("\n👤 Вы: ").strip()
            if user_input.lower() in ["exit", "quit", "выход"]:
                print("👋 Агент завершает работу.")
                break
            
            if not user_input:
                continue

            print("\n🤖 Агент думает...")
            response = agent.invoke({"input": user_input})
            
            print("\n🤖 Агент:")
            if "output" in response:
                print(response["output"])
            else:
                print("Задача выполнена.")

        except KeyboardInterrupt:
            print("\n⚠️ Прервано пользователем.")
            break
        except Exception as e:
            print(f"\n❌ Ошибка: {e}")


if __name__ == "__main__":
    main()
