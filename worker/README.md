Here, you can find the documentation for the worker component of our system. The worker is responsible for processing tasks and performing computations as needed. It interacts with other components to receive tasks, execute them, and return results.

this will be written in `python`

for the RAG part. i won't focus on it at first and it's an optional complexity for now.

---

my firts thinking about the workers that each worker will be a process that will be spawned by the agent on the master's request. each worker will register itself with the master on startup and will maintain an in-memory priority queue to manage incoming tasks. the worker will process tasks based on their priority, with pro users' tasks being processed before free users' tasks.

but for the worker implementation, i don't really know. i was thinking about running `ollama` instance in each worker. but will this be effiecient? This requires more research

what i am seeing now is the best thing is to start a single ollama instance by the agent for each machine
