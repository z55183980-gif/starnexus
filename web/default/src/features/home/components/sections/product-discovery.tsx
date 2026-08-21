/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { AnimateInView } from '@/components/animate-in-view'

export function ProductDiscovery() {
  const { t } = useTranslation()

  const audiences = [
    {
      id: 'developers',
      title: t('Application developers'),
      description: t(
        'Connect AI applications through a unified API base URL and switch models with less integration work.'
      ),
      points: [
        t('OpenAI-compatible API workflows'),
        t('Multiple model families through one account'),
        t('Usage records and billing visibility'),
      ],
    },
    {
      id: 'agents',
      title: t('AI coding and agent users'),
      description: t(
        'Follow documented setup guides for Codex, OpenClaw, Hermes Agent, CC Switch, and other compatible clients.'
      ),
      points: [
        t('Dedicated client setup guides'),
        t('A consistent API base URL'),
        t('Current model selection in the console'),
      ],
    },
    {
      id: 'teams',
      title: t('Teams managing AI usage'),
      description: t(
        'Create and manage API keys, review usage records, and understand model pricing from one account.'
      ),
      points: [
        t('Centralized API key management'),
        t('Request logs and usage tracking'),
        t('Model pricing and quota information'),
      ],
    },
  ]

  const faqs = [
    {
      id: 'what-is',
      question: t('What is StarNexus?'),
      answer: t(
        'StarNexus is the AI API service operated at DKBY.com. It provides a unified gateway and account console; it is not the official website of OpenAI, Anthropic, Google, or any model provider.'
      ),
    },
    {
      id: 'models',
      question: t('Which models and API protocols are supported?'),
      answer: t(
        'The platform provides OpenAI-compatible access and supports Claude, Gemini, DeepSeek, and other model workflows. Available models can change, so check the live model list and pricing before use.'
      ),
      link: <Link to='/pricing'>{t('View available models and pricing')}</Link>,
    },
    {
      id: 'start',
      question: t('How do I start using the API?'),
      answer: t(
        'Create an account, generate an API key, choose an available model, configure the API base URL according to the documentation, and then monitor usage in the console.'
      ),
      link: (
        <a
          href='https://docs.dkby.com/docs/doc/step1.html'
          target='_blank'
          rel='noreferrer'
        >
          {t('Read the API key guide')}
        </a>
      ),
    },
    {
      id: 'tools',
      question: t('Can I use StarNexus with Codex, OpenClaw, or Hermes Agent?'),
      answer: t(
        'Yes. The documentation center provides dedicated setup guides for Codex, OpenClaw, Hermes Agent, CC Switch, and related tools.'
      ),
      link: (
        <a
          href='https://docs.dkby.com/docs/help.html'
          target='_blank'
          rel='noreferrer'
        >
          {t('Read the integration documentation')}
        </a>
      ),
    },
    {
      id: 'pricing',
      question: t('How are pricing and availability determined?'),
      answer: t(
        'Model availability, groups, rates, and billing rules may change. Treat the live pricing page and account console as the current source of truth.'
      ),
    },
  ]

  return (
    <section
      aria-labelledby='product-discovery-title'
      className='border-border/40 relative z-10 border-t px-6 py-24 md:py-32'
    >
      <div className='mx-auto flex max-w-6xl flex-col gap-16'>
        <AnimateInView className='mx-auto max-w-3xl text-center'>
          <p className='text-muted-foreground mb-3 text-xs font-medium tracking-widest uppercase'>
            {t('Product overview')}
          </p>
          <h2
            id='product-discovery-title'
            className='text-2xl font-bold tracking-tight md:text-3xl'
          >
            {t('One API for multiple AI models')}
          </h2>
          <p className='text-muted-foreground mt-4 text-sm leading-relaxed md:text-base'>
            {t(
              'StarNexus is a unified AI API gateway that helps developers and teams access and manage OpenAI, Claude, Gemini, DeepSeek, and other models through compatible interfaces.'
            )}
          </p>
        </AnimateInView>

        <div>
          <h2 className='mb-8 text-center text-xl font-semibold tracking-tight md:text-2xl'>
            {t('Who is StarNexus for?')}
          </h2>
          <div className='grid gap-4 md:grid-cols-3'>
            {audiences.map((audience, index) => (
              <AnimateInView
                key={audience.id}
                delay={index * 100}
                animation='fade-up'
                className='h-full'
              >
                <Card className='h-full'>
                  <CardHeader>
                    <CardTitle>
                      <h3>{audience.title}</h3>
                    </CardTitle>
                    <CardDescription>{audience.description}</CardDescription>
                  </CardHeader>
                  <CardContent>
                    <ul className='text-muted-foreground flex list-disc flex-col gap-2 pl-5 text-sm'>
                      {audience.points.map((point) => (
                        <li key={point}>{point}</li>
                      ))}
                    </ul>
                  </CardContent>
                </Card>
              </AnimateInView>
            ))}
          </div>
        </div>

        <AnimateInView animation='fade-up'>
          <Card>
            <CardHeader>
              <CardTitle>
                <h2 className='text-xl md:text-2xl'>
                  {t('Common questions about StarNexus')}
                </h2>
              </CardTitle>
              <CardDescription>
                {t(
                  'Clear answers about the product, supported workflows, setup, and current pricing information.'
                )}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <Accordion defaultValue={['what-is']}>
                {faqs.map((faq) => (
                  <AccordionItem key={faq.id} value={faq.id}>
                    <AccordionTrigger>{faq.question}</AccordionTrigger>
                    <AccordionContent className='text-muted-foreground'>
                      <p>{faq.answer}</p>
                      {faq.link ? <p>{faq.link}</p> : null}
                    </AccordionContent>
                  </AccordionItem>
                ))}
              </Accordion>
            </CardContent>
          </Card>
        </AnimateInView>
      </div>
    </section>
  )
}
